/*
Copyright IBM Corp. 2016 All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

		 http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package chaincode

import (
	"fmt"
	"io"
	"sync"
	"time"

	ccintf "github.com/abchain/fabric/core/chaincode/container/ccintf"
	cred "github.com/abchain/fabric/core/cred"
	prim "github.com/abchain/fabric/core/crypto/primitives"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/core/util"
	pb "github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
	"github.com/looplab/fsm"
	"github.com/op/go-logging"
	"golang.org/x/net/context"

	"github.com/abchain/fabric/core/ledger"
	"math/rand"
)

const (
	createdstate     = "created"     //start state
	establishedstate = "established" //in: CREATED, rcv:  REGISTER, send: REGISTERED, INIT
	initstate        = "init"        //in:ESTABLISHED, rcv:-, send: INIT
	readystate       = "ready"       //in:ESTABLISHED,TRANSACTION, rcv:COMPLETED
	transactionstate = "transaction" //in:READY, rcv: xact from consensus, send: TRANSACTION
	busyinitstate    = "busyinit"    //in:INIT, rcv: PUT_STATE, DEL_STATE, INVOKE_CHAINCODE
	busystate        = "busy"        //in:QUERY, rcv: GET_STATE
	busyxactstate    = "busyxact"    //in:TRANSACION, rcv: GET_STATE, PUT_STATE, DEL_STATE, INVOKE_CHAINCODE
	endstate         = "end"         //in:INIT,ESTABLISHED, rcv: error, terminate container

	//the maxium of streams a handler can hold
	maxStreamCount = 16
)

var CCHandlingErr_RCMain = fmt.Errorf("Chaincode handling have a racing condiction")
var CCHandlingErr_RCWrite = fmt.Errorf("Chaincode handling have a racing condiction in writting states")

var chaincodeLogger = logging.MustGetLogger("chaincode")

// MessageHandler interface for handling chaincode messages (common between Peer chaincode support and chaincode)
type MessageHandler interface {
	HandleMessage(msg *pb.ChaincodeMessage) error
	SendMessage(msg *pb.ChaincodeMessage) error
}

type transactionResult struct {
	Error   error
	Payload []byte
	State   ledger.TxExecStates
}

type transactionContext struct {
	execContext      context.Context
	isTransaction    bool
	state            ledger.TxExecStates
	inputMsg         *pb.ChaincodeMessage
	responseNotifier chan *pb.ChaincodeMessage

	// tracks open iterators used for range queries
	rangeQueryIteratorMap map[string]statemgmt.RangeScanIterator
	encryptor             cred.DataEncryptor
}

func (tctx *transactionContext) failTx(err error) {

	tctx.responseNotifier <- &pb.ChaincodeMessage{
		Type:    pb.ChaincodeMessage_ERROR,
		Payload: []byte(err.Error()),
		Txid:    tctx.inputMsg.Txid}
}

func (tctx *transactionContext) decrypt(payload []byte) ([]byte, error) {
	if tctx.encryptor == nil {
		return payload, nil
	}
	return tctx.encryptor.Decrypt(payload)
}

func (tctx *transactionContext) encrypt(payload []byte) ([]byte, error) {
	if tctx.encryptor == nil {
		return payload, nil
	}
	return tctx.encryptor.Encrypt(payload)
}

//with txnetwork we should not need binding (which is designed for resisting copy attacking)
//anymore, we just add one for compatible
func (handler *Handler) getSecurityBinding(tx *pb.Transaction) ([]byte, error) {
	return prim.Hash(append(tx.Cert, tx.Nonce...)), nil
}

type workingStream struct {
	ccintf.ChaincodeStream
	serialId   int
	tctxs      map[string]*transactionContext
	resp       chan *pb.ChaincodeMessage
	exitNotify chan interface{}

	Incoming chan *transactionContext
	Acking   chan string
}

func (ws *workingStream) finishTx(userCancel bool, tctx *transactionContext, handler *Handler) {

	//must clean query iterator
	for _, iter := range tctx.rangeQueryIteratorMap {
		iter.Close()
	}

	txid := tctx.inputMsg.Txid
	delete(ws.tctxs, txid)
	if tctx.isTransaction {
		if userCancel {
			//send a ERROR resp to chaincode, or the cc side will fail (response if omitted)
			ws.Send(&pb.ChaincodeMessage{Type: pb.ChaincodeMessage_ERROR, Payload: []byte("UserCancel"), Txid: txid})
		}
	}
}

func (ws *workingStream) handleResp(msg *pb.ChaincodeMessage) {
	if ws.exitNotify == nil {
		panic("Touch resp before message processing has begun, wrong code")
	}

	select {
	case <-ws.exitNotify:
	case ws.resp <- msg:
	}
}

func (ws *workingStream) handleReadState(msg *pb.ChaincodeMessage, tctx *transactionContext, handler *Handler) {

	chaincodeLogger.Debugf("[%s]Received %s, invoking reading states from ledger", shorttxid(msg.Txid), msg.Type)

	var respmsg *pb.ChaincodeMessage
	var err error

	switch msg.Type {
	case pb.ChaincodeMessage_GET_STATE:
		respmsg, err = handler.handleGetState(msg, tctx)
	case pb.ChaincodeMessage_RANGE_QUERY_STATE:
		respmsg, err = handler.handleRangeQueryState(msg, tctx)
	case pb.ChaincodeMessage_RANGE_QUERY_STATE_NEXT:
		respmsg, err = handler.handleRangeQueryStateNext(msg, tctx)
	case pb.ChaincodeMessage_RANGE_QUERY_STATE_CLOSE:
		respmsg, err = handler.handleRangeQueryStateClose(msg, tctx)
	default:
		err = fmt.Errorf("Unrecognized query msg type %s", msg.Type)
	}

	if err != nil {
		ws.handleResp(&pb.ChaincodeMessage{Type: pb.ChaincodeMessage_ERROR, Payload: []byte(err.Error()), Txid: msg.Txid})
	} else {
		ws.handleResp(respmsg)
	}
}

func (ws *workingStream) handleWriteState(msg *pb.ChaincodeMessage, tctx *transactionContext, handler *Handler) {

	chaincodeLogger.Debugf("[%s]Received %s, invoking write states from ledger", shorttxid(msg.Txid), msg.Type)

	var respmsg *pb.ChaincodeMessage
	var err error

	switch msg.Type {
	case pb.ChaincodeMessage_PUT_STATE, pb.ChaincodeMessage_DEL_STATE:
		respmsg, err = handler.handlePutState(msg, tctx)
	default:
		err = fmt.Errorf("Unrecognized query msg type %s", msg.Type)
	}

	if err != nil {
		ws.handleResp(&pb.ChaincodeMessage{Type: pb.ChaincodeMessage_ERROR, Payload: []byte(err.Error()), Txid: msg.Txid})
	} else {
		ws.handleResp(respmsg)
	}
}

func (ws *workingStream) handleInvokeChaincode(msg *pb.ChaincodeMessage, tctx *transactionContext, handler *Handler) {

	chaincodeLogger.Debugf("[%s]Received %s, invoking another chaincode invoking", shorttxid(msg.Txid), msg.Type)

	var respmsg *pb.ChaincodeMessage
	var err error

	switch msg.Type {
	case pb.ChaincodeMessage_INVOKE_CHAINCODE,
		pb.ChaincodeMessage_INVOKE_QUERY:
		respmsg, err = handler.handleInvokeChaincode(msg, tctx)
	default:
		err = fmt.Errorf("Unrecognized query msg type %s", msg.Type)
	}

	if err != nil {
		ws.handleResp(&pb.ChaincodeMessage{Type: pb.ChaincodeMessage_ERROR, Payload: []byte(err.Error()), Txid: msg.Txid})
	} else {
		ws.handleResp(respmsg)
	}
}

func (ws *workingStream) handleMessage(msg *pb.ChaincodeMessage, tctx *transactionContext, handler *Handler) {

	//we dance most routine into another thread EXCEPT for the completed routine,
	//because finishTx is thread-unsafe (the tctxs map is touched)
	//another receiving must be spined off after the handling routine so the
	//handling of msg from cc is serial
	switch msg.Type {
	case pb.ChaincodeMessage_GET_STATE,
		pb.ChaincodeMessage_RANGE_QUERY_STATE,
		pb.ChaincodeMessage_RANGE_QUERY_STATE_NEXT,
		pb.ChaincodeMessage_RANGE_QUERY_STATE_CLOSE:
		go ws.handleReadState(msg, tctx, handler)

	case pb.ChaincodeMessage_PUT_STATE,
		pb.ChaincodeMessage_DEL_STATE:
		go ws.handleWriteState(msg, tctx, handler)

	case pb.ChaincodeMessage_INVOKE_CHAINCODE,
		pb.ChaincodeMessage_INVOKE_QUERY:
		go ws.handleInvokeChaincode(msg, tctx, handler)

	case pb.ChaincodeMessage_QUERY_COMPLETED:
		//need to encrypt the result
		if payload, err := tctx.encrypt(msg.Payload); nil != err {
			chaincodeLogger.Errorf("[%s]Failed to encrypt query result %s", shorttxid(msg.Txid), string(msg.Payload))
			msg.Payload = []byte(fmt.Sprintf("Failed to encrypt query result %s", err.Error()))
			msg.Type = pb.ChaincodeMessage_QUERY_ERROR
		} else {
			msg.Payload = payload
		}
		fallthrough
	case pb.ChaincodeMessage_COMPLETED,
		pb.ChaincodeMessage_ERROR,
		pb.ChaincodeMessage_QUERY_ERROR:
		chaincodeLogger.Debugf("[%s]HandleMessage-_COMPLETED. Notify", shorttxid(msg.Txid))
		tctx.responseNotifier <- msg
		ws.finishTx(false, tctx, handler)
	default:
		panic(fmt.Errorf("Unrecognized msg type: %s", msg.Type))
	}
}

func (ws *workingStream) processStream(ctx context.Context, handler *Handler) (err error) {
	msgAvail := make(chan *pb.ChaincodeMessage)
	ws.exitNotify = make(chan interface{})

	var ioerr error
	defer handler.streamLeave(ws)
	defer func() {
		close(ws.exitNotify)
	}()

	recvF := func() {
		in, err := ws.Recv()
		ioerr = err
		select {
		case msgAvail <- in:
		case <-ws.exitNotify:
			return
		}
	}

	go recvF()

	for {
		select {
		case <-ctx.Done():
			chaincodeLogger.Debugf("External signal ending of chaincode support stream")
			return ctx.Err()
		case in := <-msgAvail:
			if ioerr == io.EOF {
				chaincodeLogger.Debugf("Received EOF, ending chaincode support stream")
				return ioerr
			} else if ioerr != nil {
				chaincodeLogger.Errorf("Error handling chaincode support stream: %s", ioerr)
				return ioerr
			} else if in == nil {
				return fmt.Errorf("Received nil message, ending chaincode support stream")
			}
			chaincodeLogger.Debugf("[%s]Received message %s from shim", shorttxid(in.Txid), in.Type.String())
			if in.Type == pb.ChaincodeMessage_ERROR {
				chaincodeLogger.Errorf("Got error: %s", string(in.Payload))
			}

			//just filter keepalive ...
			if in.Type == pb.ChaincodeMessage_KEEPALIVE {
				chaincodeLogger.Debug("Received KEEPALIVE Response")
				// Received a keep alive message, we don't do anything with it for now
				// and it does not touch the state machine
				go recvF()
				continue
			}

			if tctx, ok := ws.tctxs[in.Txid]; ok {
				//recv will be triggered among handleMessage
				ws.handleMessage(in, tctx, handler)
			} else {
				chaincodeLogger.Error("Received message from unknown/deleted tx:", in.Txid)
				//for the "finish message", just omit it, or simply replay and not care error
				switch in.Type {
				case pb.ChaincodeMessage_QUERY_COMPLETED,
					pb.ChaincodeMessage_COMPLETED,
					pb.ChaincodeMessage_ERROR,
					pb.ChaincodeMessage_QUERY_ERROR:
					//no response on ending message
				default:
					ws.Send(&pb.ChaincodeMessage{Type: pb.ChaincodeMessage_ERROR, Payload: []byte("Unknown tx"), Txid: in.Txid})
				}
			}
			go recvF()

		case out := <-ws.resp:
			chaincodeLogger.Debugf("[%s]Sending message %s to shim", shorttxid(out.Txid), out.Type.String())
			if err := ws.Send(out); err != nil {
				chaincodeLogger.Errorf("stream on tx[%s] sending and received error: %s",
					shorttxid(out.Txid), err)
				return err
			}
		case txid := <-ws.Acking:
			if tctx, ok := ws.tctxs[txid]; ok {
				//tx is ONLY removed here (so the invoker must respond for it)
				chaincodeLogger.Debugf("[%s]Tx (is invoking: %v) is cancelled", shorttxid(txid), tctx.isTransaction)
				//TODO: current we can't cancel tx in chaincode_sp side or we always
				//broken the FSM in chaincode side and finally lead to a failure
				//in the stream ...
				//ws.finishTx(true, tctx, handler)
			}
		case tctxin := <-ws.Incoming:
			txid := tctxin.inputMsg.GetTxid()
			if _, ok := ws.tctxs[txid]; ok {
				//duplicated tx
				tctxin.failTx(fmt.Errorf("Duplicated Tx"))
				continue
			}

			msg := tctxin.inputMsg
			if err := ws.Send(msg); err != nil {
				chaincodeLogger.Errorf("stream on tx[%s] sending and received error: %s",
					shorttxid(msg.Txid), err)
				return err
			}

			chaincodeLogger.Debugf("[%s]Sending new tx %s [tx:%v] to shim", shorttxid(msg.Txid), msg.Type.String(), tctxin.isTransaction)
			ws.tctxs[txid] = tctxin
			if tctxin.state.Uninited() {
				//here is the legacy supproting for old executing of tx
				//later we may just panic it
				if tctxin.isTransaction {
					tctxin.state.InitForInvoking(handler.Ledger)
				} else {
					tctxin.state.InitForQuerying(handler.Ledger)
				}
			}

		case <-handler.waitForKeepaliveTimer():
			if handler.chaincodeSupport.keepalive <= 0 {
				chaincodeLogger.Errorf("Invalid select: keepalive not on (keepalive=%d)", handler.chaincodeSupport.keepalive)
				continue
			}

			//TODO we could use this to hook into container lifecycle (kill the chaincode if not in use, etc)
			if err := ws.Send(&pb.ChaincodeMessage{Type: pb.ChaincodeMessage_KEEPALIVE}); err != nil {
				return fmt.Errorf("Error sending keepalive, err=%s", err)
			} else {
				chaincodeLogger.Debug("Sent KEEPALIVE request")
			}
		}
	}
}

// Handler responsbile for management of Peer's side of chaincode stream
type Handler struct {
	sync.RWMutex
	workingStream []*workingStream
	FSM           *fsm.FSM
	ChaincodeID   *pb.ChaincodeID
	Ledger        *ledger.Ledger
	ShimVersion   int

	// A copy of decrypted deploy tx this handler manages, no code
	deployTXSecContext *pb.Transaction

	chaincodeSupport *ChaincodeSupport
}

func shorttxid(txid string) string {
	if len(txid) < 8 {
		return txid
	}
	return txid[0:8]
}

func filterFSMError(err error) error {
	switch err.(type) {
	case fsm.NoTransitionError:
		return nil
	default:
		return err
	}
}

// createTransactionMessage creates a transaction message.
func createTransactionMessage(txtype pb.Transaction_Type, txid string, cMsg *pb.ChaincodeInput, sec *pb.ChaincodeSecurityContext) (*pb.ChaincodeMessage, error) {
	payload, err := proto.Marshal(cMsg)
	if err != nil {
		return nil, err
	}

	msgType := pb.ChaincodeMessage_QUERY
	switch txtype {
	case pb.Transaction_CHAINCODE_INVOKE:
		msgType = pb.ChaincodeMessage_TRANSACTION
	case pb.Transaction_CHAINCODE_DEPLOY:
		msgType = pb.ChaincodeMessage_INIT
	}

	if sec == nil {
		//create a default, blank sec context, which include a variat (not consistent) timestamp
		sec = new(pb.ChaincodeSecurityContext)
		sec.TxTimestamp = util.CreateUtcTimestamp()
		chaincodeLogger.Debug("setting a blank chaincode security context")
	}

	return &pb.ChaincodeMessage{Type: msgType, Payload: payload, Txid: txid, SecurityContext: sec}, nil
}

func cloneTx(tx *pb.Transaction) (*pb.Transaction, error) {
	raw, err := proto.Marshal(tx)
	if err != nil {
		chaincodeLogger.Errorf("Failed marshalling transaction [%s].", err.Error())
		return nil, err
	}

	clone := &pb.Transaction{}
	err = proto.Unmarshal(raw, clone)
	if err != nil {
		chaincodeLogger.Errorf("Failed unmarshalling transaction [%s].", err.Error())
		return nil, err
	}

	return clone, nil
}

// strip the required part of transactions for currently deployment, including the
// data required for confiedantiality, txid, etc ...
func strippedTxForDeployment(tx *pb.Transaction) (*pb.Transaction, error) {
	ret, err := cloneTx(tx)
	if err != nil {
		return nil, err
	}

	//don't need the payload and other fields which is not useful and rather large
	ret.Payload = nil
	ret.Signature = nil
	ret.Metadata = nil
	ret.Nonce = nil

	// //should not include path in ccid anymore
	// //we need to null out path from depTx as invoke or queries don't have it
	// cID := &pb.ChaincodeID{}
	// err := proto.Unmarshal(handler.deployTXSecContext.ChaincodeID, cID)
	// if err != nil {
	// 	return nil, fmt.Errorf("Failed to unmarshall : %s\n", err)
	// }

	// cID.Path = ""
	// data, err := proto.Marshal(cID)
	// if err != nil {
	// 	return nil, fmt.Errorf("Failed to marshall : %s\n", err)
	// }

	// ret.ChaincodeID = data
	return ret, nil
}

func (handler *Handler) waitForKeepaliveTimer() <-chan time.Time {
	if handler.chaincodeSupport.keepalive > 0 {
		c := time.After(handler.chaincodeSupport.keepalive)
		return c
	}
	//no one will signal this channel, listner blocks forever
	c := make(chan time.Time, 1)
	return c
}

func newWorkingStream(handler *Handler, peerChatStream ccintf.ChaincodeStream) *workingStream {

	w := &workingStream{
		ChaincodeStream: peerChatStream,
		tctxs:           make(map[string]*transactionContext),
		resp:            make(chan *pb.ChaincodeMessage),
		Incoming:        make(chan *transactionContext, maxStreamCount),
		Acking:          make(chan string, maxStreamCount),
	}

	return w
}

func newChaincodeSupportHandler(chaincodeSupport *ChaincodeSupport) *Handler {
	v := &Handler{}
	v.chaincodeSupport = chaincodeSupport

	//FSM is used for filter each incoming/outcoming msg
	v.FSM = fsm.NewFSM(
		establishedstate,
		fsm.Events{
			{Name: pb.ChaincodeMessage_INIT.String(), Src: []string{establishedstate}, Dst: initstate},
			{Name: pb.ChaincodeMessage_READY.String(), Src: []string{establishedstate}, Dst: readystate},
			{Name: pb.ChaincodeMessage_QUERY.String(), Src: []string{readystate}, Dst: readystate},
			{Name: pb.ChaincodeMessage_TRANSACTION.String(), Src: []string{readystate}, Dst: readystate},
			{Name: pb.ChaincodeMessage_COMPLETED.String(), Src: []string{initstate, readystate}, Dst: readystate},
			{Name: pb.ChaincodeMessage_QUERY_COMPLETED.String(), Src: []string{readystate}, Dst: readystate},
			{Name: pb.ChaincodeMessage_ERROR.String(), Src: []string{initstate}, Dst: endstate},
			{Name: pb.ChaincodeMessage_ERROR.String(), Src: []string{readystate}, Dst: readystate},
			{Name: pb.ChaincodeMessage_QUERY_ERROR.String(), Src: []string{readystate}, Dst: readystate},
		},
		fsm.Callbacks{
		// "before_" + pb.ChaincodeMessage_REGISTER.String():               func(e *fsm.Event) { v.beforeRegisterEvent(e, v.FSM.Current()) },
		// "after_" + pb.ChaincodeMessage_INVOKE_CHAINCODE.String():        func(e *fsm.Event) { v.afterInvokeChaincode(e, v.FSM.Current()) },
		// "enter_" + establishedstate:                                     func(e *fsm.Event) { v.enterEstablishedState(e, v.FSM.Current()) },
		},
	)

	return v
}

func (handler *Handler) addNewStream(stream ccintf.ChaincodeStream) (*workingStream, error) {

	//well, if handler.workingStream is empty, its len must not exceed maxStreamCount
	if handler.FSM.Current() != readystate && len(handler.workingStream) != 0 {
		return nil, fmt.Errorf("Not allow more than one stream when prepared")
	} else if len(handler.workingStream) >= maxStreamCount {
		return nil, fmt.Errorf("Exceed maxium stream limit (%d)", maxStreamCount)
	}
	ws := newWorkingStream(handler, stream)
	ws.serialId = len(handler.workingStream)
	handler.workingStream = append(handler.workingStream, ws)

	chaincodeLogger.Debugf("stream [%d] is added into handler [%s]", ws.serialId, handler.ChaincodeID.Name)
	return ws, nil
}

func (handler *Handler) streamLeave(ws *workingStream) {

	handler.Lock()

	//remove ws from handler
	handler.workingStream = append(handler.workingStream[:ws.serialId],
		handler.workingStream[ws.serialId+1:]...)

	for _, wsm := range handler.workingStream[ws.serialId:] {
		wsm.serialId = wsm.serialId - 1
	}

	stremLeft := len(handler.workingStream)

	chaincodeLogger.Debugf("Stream %d left, handler %s have %d streams left", ws.serialId, handler.ChaincodeID.Name, stremLeft)
	handler.Unlock()

	//fail all of the contexts
	for txid, tctx := range ws.tctxs {
		tctx.responseNotifier <- &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_ERROR,
			Payload: []byte("Stream Failure"), Txid: txid}
	}
	//and "exhaust" all in the incoming/acking channel
	go func() {
		for {
			select {
			case <-ws.Incoming:
			case <-ws.Acking:
			default:
				return
			}
		}
	}()

	//if all stream is out, we de-reg it and expect another lauching may resume the case...
	if stremLeft == 0 {
		handler.chaincodeSupport.deregisterHandler(handler)
	}

}

func (handler *Handler) queryToStream(tctx *transactionContext) (*workingStream, error) {
	handler.RLock()
	if len(handler.workingStream) == 0 {
		handler.RUnlock()
		return nil, fmt.Errorf("No availiable workstream")
	}

	ws := handler.workingStream[rand.Intn(len(handler.workingStream))]
	handler.RUnlock()

	ws.Incoming <- tctx
	return ws, nil
}

func (handler *Handler) handleGetState(msg *pb.ChaincodeMessage, tctx *transactionContext) (*pb.ChaincodeMessage, error) {

	key := string(msg.Payload)
	ledgerObj := handler.Ledger

	// Invoke ledger to get state
	chaincodeID := handler.ChaincodeID.Name
	res, err := tctx.state.Get(chaincodeID, key, ledgerObj)

	if err != nil {
		// Send error msg back to chaincode. GetState will not trigger event
		chaincodeLogger.Errorf("[%s]Failed to get chaincode state(%s). Sending %s", shorttxid(msg.Txid), err, pb.ChaincodeMessage_ERROR)
		return nil, err
	} else if res == nil {
		//The state object being requested does not exist, so don't attempt to decrypt it
		chaincodeLogger.Debugf("[%s]No state associated with key: %s. Sending %s with an empty payload", shorttxid(msg.Txid), key, pb.ChaincodeMessage_RESPONSE)
		return &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_RESPONSE, Txid: msg.Txid}, nil
	} else {
		// Decrypt the data if the confidential is enabled
		if res, err = tctx.decrypt(res); err == nil {
			// Send response msg back to chaincode. GetState will not trigger event
			chaincodeLogger.Debugf("[%s]Got state. Sending %s", shorttxid(msg.Txid), pb.ChaincodeMessage_RESPONSE)
			return &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_RESPONSE, Payload: res, Txid: msg.Txid}, nil
		} else {
			// Send err msg back to chaincode.
			chaincodeLogger.Errorf("[%s]Got error (%s) while decrypting. Sending %s", shorttxid(msg.Txid), err, pb.ChaincodeMessage_ERROR)
			return nil, err
		}

	}

}

const maxRangeQueryStateLimit = 100

// Handles query to ledger to rage query state next
func (handler *Handler) handleRangeQuery(rangeIter statemgmt.RangeScanIterator, iterID string, tctx *transactionContext) (*pb.ChaincodeMessage, error, bool) {

	var keysAndValues []*pb.RangeQueryStateKeyValue
	var i = uint32(0)
	hasNext := rangeIter.Next()
	txid := tctx.inputMsg.GetTxid()
	for ; hasNext && i < maxRangeQueryStateLimit; i++ {
		key, value := rangeIter.GetKeyValue()
		// Decrypt the data if the confidential is enabled
		decryptedValue, decryptErr := tctx.decrypt(value)
		if decryptErr != nil {
			chaincodeLogger.Errorf("Failed decrypt value. Sending %s", pb.ChaincodeMessage_ERROR)

			return nil, decryptErr, false
		}
		keyAndValue := pb.RangeQueryStateKeyValue{Key: key, Value: decryptedValue}
		keysAndValues = append(keysAndValues, &keyAndValue)

		hasNext = rangeIter.Next()
	}

	payload := &pb.RangeQueryStateResponse{KeysAndValues: keysAndValues, HasMore: hasNext, ID: iterID}
	payloadBytes, err := proto.Marshal(payload)
	if err != nil {

		// Send error msg back to chaincode. GetState will not trigger event
		chaincodeLogger.Errorf("Failed marshall resopnse. Sending %s", pb.ChaincodeMessage_ERROR)
		return nil, err, false
	}

	chaincodeLogger.Debugf("Got keys and values. Sending %s", pb.ChaincodeMessage_RESPONSE)
	return &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_RESPONSE, Payload: payloadBytes, Txid: txid}, nil, hasNext
}

// Handles query to ledger to rage query state
func (handler *Handler) handleRangeQueryState(msg *pb.ChaincodeMessage, tctx *transactionContext) (*pb.ChaincodeMessage, error) {

	rangeQueryState := &pb.RangeQueryState{}
	unmarshalErr := proto.Unmarshal(msg.Payload, rangeQueryState)
	if unmarshalErr != nil {
		chaincodeLogger.Errorf("Failed to unmarshall range query request. Sending %s", pb.ChaincodeMessage_ERROR)
		return nil, unmarshalErr
	}

	chaincodeID := handler.ChaincodeID.Name
	ledgerObj := handler.Ledger

	rangeIter, err := tctx.state.GetRangeScanIterator(chaincodeID, rangeQueryState.StartKey, rangeQueryState.EndKey, ledgerObj)

	if err != nil {
		// Send error msg back to chaincode. GetState will not trigger event
		chaincodeLogger.Errorf("Failed to get ledger scan iterator. Sending %s", pb.ChaincodeMessage_ERROR)
		return nil, err
	}

	iterID := util.GenerateUUID()
	tctx.rangeQueryIteratorMap[iterID] = rangeIter

	ret, err, hasNext := handler.handleRangeQuery(rangeIter, iterID, tctx)
	if !hasNext {
		rangeIter.Close()
		delete(tctx.rangeQueryIteratorMap, iterID)
	}

	return ret, err

}

// Handles query to ledger to rage query state next
func (handler *Handler) handleRangeQueryStateNext(msg *pb.ChaincodeMessage, tctx *transactionContext) (*pb.ChaincodeMessage, error) {

	rangeQueryStateNext := &pb.RangeQueryStateNext{}
	unmarshalErr := proto.Unmarshal(msg.Payload, rangeQueryStateNext)
	if unmarshalErr != nil {
		chaincodeLogger.Errorf("Failed to unmarshall state range next query request. Sending %s", pb.ChaincodeMessage_ERROR)
		return nil, unmarshalErr
	}

	rangeIter := tctx.rangeQueryIteratorMap[rangeQueryStateNext.ID]

	if rangeIter == nil {
		return nil, fmt.Errorf("Range query iterator not found")
	}

	ret, err, hasNext := handler.handleRangeQuery(rangeIter, rangeQueryStateNext.ID, tctx)
	if !hasNext {
		rangeIter.Close()
		delete(tctx.rangeQueryIteratorMap, rangeQueryStateNext.ID)
	}

	return ret, err

}

// Handles the closing of a state iterator
func (handler *Handler) handleRangeQueryStateClose(msg *pb.ChaincodeMessage, tctx *transactionContext) (*pb.ChaincodeMessage, error) {

	rangeQueryStateClose := &pb.RangeQueryStateClose{}
	unmarshalErr := proto.Unmarshal(msg.Payload, rangeQueryStateClose)
	if unmarshalErr != nil {
		chaincodeLogger.Errorf("Failed to unmarshall state range query close request. Sending %s", pb.ChaincodeMessage_ERROR)
		return nil, unmarshalErr
	}

	iter := tctx.rangeQueryIteratorMap[rangeQueryStateClose.ID]
	if iter != nil {
		iter.Close()
		delete(tctx.rangeQueryIteratorMap, rangeQueryStateClose.ID)
	}

	payload := &pb.RangeQueryStateResponse{HasMore: false, ID: rangeQueryStateClose.ID}
	payloadBytes, err := proto.Marshal(payload)
	if err != nil {

		// Send error msg back to chaincode. GetState will not trigger event
		chaincodeLogger.Errorf("Failed marshall resopnse. Sending %s", pb.ChaincodeMessage_ERROR)
		return nil, err
	}

	chaincodeLogger.Debugf("Closed. Sending %s", pb.ChaincodeMessage_RESPONSE)
	return &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_RESPONSE, Payload: payloadBytes, Txid: msg.Txid}, nil

}

func (handler *Handler) handlePutState(msg *pb.ChaincodeMessage, tctx *transactionContext) (*pb.ChaincodeMessage, error) {

	chaincodeID := handler.ChaincodeID.Name
	var err error

	if msg.Type == pb.ChaincodeMessage_PUT_STATE {
		putStateInfo := &pb.PutStateInfo{}
		unmarshalErr := proto.Unmarshal(msg.Payload, putStateInfo)
		if unmarshalErr != nil {
			chaincodeLogger.Debugf("[%s]Unable to decipher payload. Sending %s", shorttxid(msg.Txid), pb.ChaincodeMessage_ERROR)
			return nil, unmarshalErr
		}

		if putStateInfo.GetKey() == "" || putStateInfo.GetValue() == nil {
			return nil, fmt.Errorf("An empty string key or a nil value is not supported")
		}

		var pVal []byte
		// Encrypt the data if the confidential is enabled
		if pVal, err = tctx.encrypt(putStateInfo.Value); err == nil {
			tctx.state.Set(chaincodeID, putStateInfo.GetKey(), pVal)
		}
	} else if msg.Type == pb.ChaincodeMessage_DEL_STATE {
		tctx.state.Delete(chaincodeID, string(msg.Payload))
	}

	if err != nil {
		// Send error msg back to chaincode and trigger event
		chaincodeLogger.Errorf("[%s]Failed to handle %s. Sending %s", shorttxid(msg.Txid), msg.Type.String(), pb.ChaincodeMessage_ERROR)
		return nil, err
	}

	// Send response msg back to chaincode.
	chaincodeLogger.Debugf("[%s]Completed %s. Sending %s", shorttxid(msg.Txid), msg.Type.String(), pb.ChaincodeMessage_RESPONSE)
	return &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_RESPONSE, Txid: msg.Txid}, nil

}

func (handler *Handler) handleInvokeChaincode(msg *pb.ChaincodeMessage, tctx *transactionContext) (*pb.ChaincodeMessage, error) {

	chaincodeSpec := &pb.ChaincodeSpec{}
	unmarshalErr := proto.Unmarshal(msg.Payload, chaincodeSpec)
	if unmarshalErr != nil {
		chaincodeLogger.Debugf("[%s]Unable to decipher payload. Sending %s", shorttxid(msg.Txid), pb.ChaincodeMessage_ERROR)
		return nil, unmarshalErr
	}

	// never allow invoking itself!
	if chaincodeSpec.ChaincodeID.GetName() == handler.ChaincodeID.GetName() {
		chaincodeLogger.Errorf("tx [%s] cause a invoking to itself", shorttxid(msg.Txid))
		return nil, fmt.Errorf("Invoking-self failure")
	}

	var txtype pb.Transaction_Type
	if msg.Type == pb.ChaincodeMessage_INVOKE_CHAINCODE {
		if !tctx.isTransaction {
			chaincodeLogger.Errorf("tx [%s] cause a invoking in an query tx", shorttxid(msg.Txid))
			return nil, fmt.Errorf("Invoking in query tx")
		}
		txtype = pb.Transaction_CHAINCODE_INVOKE
	} else {
		if msg.Type != pb.ChaincodeMessage_INVOKE_QUERY {
			panic(fmt.Errorf("Impossible message type %s", msg.Type))
		}
		txtype = pb.Transaction_CHAINCODE_QUERY
	}

	// Launch the new chaincode if not already running
	// TODO: we should support the inter-ledger invoking
	ledgerObj := handler.Ledger
	launchErr, chrte := handler.chaincodeSupport.Launch(context.Background(), ledgerObj, chaincodeSpec.ChaincodeID, nil)
	if launchErr != nil {
		chaincodeLogger.Debugf("[%s]Failed to launch invoked chaincode. Sending %s", shorttxid(msg.Txid), pb.ChaincodeMessage_ERROR)
		//triggerNextStateMsg = &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_ERROR, Payload: payload, Txid: msg.Txid}
		return nil, launchErr
	}

	execMsg, err := createTransactionMessage(txtype, msg.Txid, chaincodeSpec.CtorMsg, tctx.inputMsg.SecurityContext)
	if err != nil {
		return nil, fmt.Errorf("error on create invoking msg: %s", err)
	}

	// Execute the chaincode
	//NOTE: when confidential C-call-C is understood, transaction should have the correct sec context for enc/dec
	response, execErr := chrte.handler.executeMessage(tctx.execContext, execMsg, tctx.encryptor, tctx.state)

	//payload is marshalled and send to the calling chaincode's shim which unmarshals and
	//sends it to chaincode
	if execErr != nil {
		return nil, execErr
	} else {
		res, err := proto.Marshal(response)
		if err != nil {
			return nil, err
		}

		return &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_RESPONSE, Payload: res, Txid: msg.Txid}, nil
	}
}

func (handler *Handler) setChaincodeSecurityContext(tx *pb.Transaction, msg *pb.ChaincodeMessage) error {
	chaincodeLogger.Debug("setting chaincode security context...")
	if msg.SecurityContext == nil {
		msg.SecurityContext = &pb.ChaincodeSecurityContext{}
	}
	if tx != nil {
		chaincodeLogger.Debug("setting chaincode security context. Transaction different from nil")
		chaincodeLogger.Debugf("setting chaincode security context. Metadata [% x]", tx.Metadata)

		msg.SecurityContext.CallerCert = tx.Cert
		msg.SecurityContext.CallerSign = tx.Signature
		binding, err := handler.getSecurityBinding(tx)
		if err != nil {
			chaincodeLogger.Errorf("Failed getting binding [%s]", err)
			return err
		}
		msg.SecurityContext.Binding = binding
		msg.SecurityContext.Metadata = tx.Metadata

		cis := &pb.ChaincodeInvocationSpec{}
		if err := proto.Unmarshal(tx.Payload, cis); err != nil {
			chaincodeLogger.Errorf("Failed getting payload [%s]", err)
			return err
		}

		ctorMsgRaw, err := proto.Marshal(cis.ChaincodeSpec.GetCtorMsg())
		if err != nil {
			chaincodeLogger.Errorf("Failed getting ctorMsgRaw [%s]", err)
			return err
		}

		msg.SecurityContext.Payload = ctorMsgRaw
		msg.SecurityContext.TxTimestamp = tx.Timestamp
	}
	return nil
}

//if depTx is nil (should be for "deploy" only) just prepare the handler
//else do a ready process
func (handler *Handler) readyChaincode(depTx *pb.Transaction) error {

	handler.deployTXSecContext = depTx

	//sanitycheck: we definitely can obtain a pending workstream, or something wrong must in our code
	if len(handler.workingStream) == 0 {
		panic("No workstream availiable, you made wrong logic in your code")
	}

	ws := handler.workingStream[0]
	//the msg is send, and then the state of FSM is changed, because chaincode
	//never response for READY message

	//TODO: add tag for ready message
	msg := &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_READY}
	if err := ws.Send(msg); err != nil {
		chaincodeLogger.Error("sending READY fail:", err)
		return err
	}
	if err := handler.FSM.Event(msg.Type.String()); filterFSMError(err) != nil {
		chaincodeLogger.Error("Send state of handler fail:", err)
		return err
	}

	return nil
}

func (handler *Handler) executeMessage(ctx context.Context, msg *pb.ChaincodeMessage, encryptor cred.DataEncryptor, state ledger.TxExecStates) (*pb.ChaincodeMessage, error) {

	//we consider this is a racing error (some txs is too early than another, or duplicated)
	if err := handler.FSM.Event(msg.Type.String()); filterFSMError(err) != nil {

		chaincodeLogger.Errorf("Set handler state for msg %s fail: %s, current [%s]", msg.Type, err, handler.FSM.Current())
		return nil, CCHandlingErr_RCMain
	}

	//this is just for hacking the stupid code in shim module, which use the "payload" field in seccontext instead
	//of the payload in message
	if handler.ShimVersion < 1 {
		msg.SecurityContext.Payload = msg.Payload
	}

	txctx := &transactionContext{
		encryptor:             encryptor,
		execContext:           ctx,
		isTransaction:         msg.Type != pb.ChaincodeMessage_QUERY,
		inputMsg:              msg,
		state:                 state,
		responseNotifier:      make(chan *pb.ChaincodeMessage, 1),
		rangeQueryIteratorMap: make(map[string]statemgmt.RangeScanIterator)}

	//can deliver to any streams
	chaincodeLogger.Debugf("[%s]sending tx trigger", shorttxid(msg.Txid))
	wsForTx, err := handler.queryToStream(txctx)
	if err != nil {
		return nil, err
	}

	var resp *pb.ChaincodeMessage
	defer func(handler *Handler) {
		if resp == nil {
			resp = &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_ERROR,
				Payload: []byte(err.Error()),
				Txid:    msg.Txid}
		}
		handler.FSM.Event(resp.Type.String(), resp)
	}(handler)

	select {
	case resp = <-txctx.responseNotifier:
	case <-ctx.Done():
		err = fmt.Errorf("Tx exec timeout: %s", ctx.Err())
		//make the chaincode give up faster
		wsForTx.Acking <- msg.Txid
		chaincodeLogger.Debugf("[%s]tx exec fail: timeout", shorttxid(msg.Txid))
	}

	return resp, err
}

/****************
func (handler *Handler) initEvent() (chan *pb.ChaincodeMessage, error) {
	if handler.responseNotifiers == nil {
		return nil,fmt.Errorf("SendMessage called before registration for Txid:%s", msg.Txid)
	}
	var notfy chan *pb.ChaincodeMessage
	handler.Lock()
	if handler.responseNotifiers[msg.Txid] != nil {
		handler.Unlock()
		return nil, fmt.Errorf("SendMessage Txid:%s exists", msg.Txid)
	}
	//note the explicit use of buffer 1. We won't block if the receiver times outi and does not wait
	//for our response
	handler.responseNotifiers[msg.Txid] = make(chan *pb.ChaincodeMessage, 1)
	handler.Unlock()

	if err := c.serialSend(msg); err != nil {
		deleteNotifier(msg.Txid)
		return nil, fmt.Errorf("SendMessage error sending %s(%s)", msg.Txid, err)
	}
	return notfy, nil
}
*******************/
