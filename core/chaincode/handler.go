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

	ccintf "github.com/abchain/fabric/core/container/ccintf"
	"github.com/abchain/fabric/core/crypto"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/core/util"
	pb "github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
	"github.com/looplab/fsm"
	"github.com/op/go-logging"
	"golang.org/x/net/context"

	"github.com/abchain/fabric/core/ledger"
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

	//custom event
	cc_op_init           = "opccinit"
	cc_transaction_done  = "txccdone"
	cc_transaction_fail  = "txccdone"
	cc_stream_join       = "ssccjoin"
	cc_stream_leave      = "ssccleave"
	cc_message_read      = "rccrd"
	cc_message_readmore  = "rccrdmore"
	cc_message_readend   = "rccrdend"
	cc_message_write     = "rccwri"
	cc_message_keepalive = "rcckpalive"
	cc_message_finish    = "rccdone"

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

type transactionContext struct {
	isTransaction         bool
	inputMsg              *pb.ChaincodeInput
	transactionSecContext *pb.Transaction
	responseNotifier      chan *pb.ChaincodeMessage

	// tracks open iterators used for range queries
	rangeQueryIteratorMap map[string]statemgmt.RangeScanIterator
}

func (tctx *transactionContext) failTx(err error) {

	tctx.responseNotifier <- &pb.ChaincodeMessage{
		Type:    pb.ChaincodeMessage_ERROR,
		Payload: []byte(err.Error()),
		Txid:    tctx.transactionSecContext.GetTxid()}
}

func (tctx *transactionContext) clean() {

	for _, iter := range tctx.rangeQueryIteratorMap {
		iter.Close()
	}
}

type workingStream struct {
	ccintf.ChaincodeStream
	ledger     *ledger.Ledger
	resp       chan *pb.ChaincodeMessage
	Incomining chan *transactionContext
}

func (ws *workingStream) handleReadState(msg *pb.ChaincodeMessage, tctx *transactionContext, handler *Handler) {

	chaincodeLogger.Debugf("[%s]Received %s, invoking reading states from ledger", shorttxid(msg.Txid), msg.Type)

	//TODO: ws should use other way to obtain the ledger object (snapshot or thread-safe object ...)
	//currently the ledger should allow concurrent query for "commited" state along with a single read-write
	//for "uncommited" state (used by invoking)
	ledger := ws.ledger

	var respmsg *pb.ChaincodeMessage
	var err error

	switch msg.Type {
	case pb.ChaincodeMessage_GET_STATE:
		respmsg, err = handler.handleGetState(ledger, msg, tctx)
	case pb.ChaincodeMessage_RANGE_QUERY_STATE:
		respmsg, err = handler.handleRangeQueryState(ledger, msg, tctx)
	case pb.ChaincodeMessage_RANGE_QUERY_STATE_NEXT:
		respmsg, err = handler.handleRangeQueryStateNext(msg, tctx)
	case pb.ChaincodeMessage_RANGE_QUERY_STATE_CLOSE:
		respmsg, err = handler.handleRangeQueryStateClose(msg, tctx)
	default:
		err = fmt.Errorf("Unrecognized query msg type %s", msg.Type)
	}

	if err != nil {
		ws.resp <- &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_ERROR, Payload: []byte(err.Error()), Txid: msg.Txid}
	} else {
		ws.resp <- respmsg
	}
}

func (ws *workingStream) handleWriteState(msg *pb.ChaincodeMessage, tctx *transactionContext, handler *Handler) {

	chaincodeLogger.Debugf("[%s]Received %s, invoking write states from ledger", shorttxid(msg.Txid), msg.Type)

	//see the comment in handleReadState
	ledger := ws.ledger

	var respmsg *pb.ChaincodeMessage
	var err error

	switch msg.Type {
	case pb.ChaincodeMessage_PUT_STATE:
		respmsg, err = handler.handlePutState(ledger, msg, tctx)
	default:
		err = fmt.Errorf("Unrecognized query msg type %s", msg.Type)
	}
	if err != nil {
		ws.resp <- &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_ERROR, Payload: []byte(err.Error()), Txid: msg.Txid}
	} else {
		ws.resp <- respmsg
	}
}

func (ws *workingStream) handleInvokeChaincode(msg *pb.ChaincodeMessage, tctx *transactionContext, handler *Handler) {

	chaincodeLogger.Debugf("[%s]Received %s, invoking another chaincode invoking", shorttxid(msg.Txid), msg.Type)

	//see the comment in handleReadState
	ledger := ws.ledger

	var respmsg *pb.ChaincodeMessage
	var err error

	switch msg.Type {
	case pb.ChaincodeMessage_INVOKE_CHAINCODE,
		pb.ChaincodeMessage_INVOKE_QUERY:
		respmsg, err = handler.handleInvokeChaincode(ledger, msg, tctx)
	default:
		err = fmt.Errorf("Unrecognized query msg type %s", msg.Type)
	}
	if err != nil {
		ws.resp <- &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_ERROR, Payload: []byte(err.Error()), Txid: msg.Txid}
	} else {
		ws.resp <- respmsg
	}
}

func (ws *workingStream) handleMessage(msg *pb.ChaincodeMessage, tctx *transactionContext, handler *Handler, recvF func()) {

	//spin off another receiving here(so the handling of msg from cc is serial)
	recvF()
}

func (ws *workingStream) recvMsg(msgAvail chan *pb.ChaincodeMessage) {
	in, err := ws.Recv()

	if err == io.EOF {
		chaincodeLogger.Debugf("Received EOF, ending chaincode support stream, %s", err)
	} else if err != nil {
		chaincodeLogger.Errorf("Error handling chaincode support stream: %s", err)
	}
	msgAvail <- in
}

func (ws *workingStream) processStream(handler *Handler) (err error) {
	msgAvail := make(chan *pb.ChaincodeMessage)
	var ioerr error
	tctxs := make(map[string]*transactionContext)
	defer handler.FSM.Event(cc_stream_leave, ws, tctxs)

	recvF := func() {
		in, err := ws.Recv()
		ioerr = err
		msgAvail <- in
	}

	go recvF()

	for {
		select {
		case in := <-msgAvail:
			newRecv = true
			if ioerr == io.EOF {
				chaincodeLogger.Debugf("Received EOF, ending chaincode support stream, %s", err)
				return ioerr
			} else if ioerr != nil {
				chaincodeLogger.Errorf("Error handling chaincode support stream: %s", err)
				return ioerr
			} else if in == nil {
				return fmt.Errorf("Received nil message, ending chaincode support stream")
			}
			chaincodeLogger.Debugf("[%s]Received message %s from shim", shorttxid(in.Txid), in.Type.String())
			if in.Type.String() == pb.ChaincodeMessage_ERROR.String() {
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

			if tctx, ok := tctxs[in.Txid]; ok {
				//recv will be triggered among handleMessage
				go ws.handleMessage(in, tctx, handler, recvF)
			} else {
				//omit this message, but we must reply error
				chaincodeLogger.Error("Received message from unknown tx:", in.Txid)
				//simply replay and not care error
				ws.Send(&pb.ChaincodeMessage{Type: pb.ChaincodeMessage_ERROR, Payload: []byte("Unknown tx"), Txid: in.Txid})
				go recvF()
			}

		case out := <-ws.resp:
			if tctx, ok := tctxs[out.Txid]; !ok {
				//message should not be sent without a handling tx context (except keepalive)
				//so we just omit it
				chaincodeLogger.Error("Try to send a message [%s] without tx context", out)
				continue
			}

			err = ws.Send(out)
			if err != nil {
				chaincodeLogger.Errorf("stream on tx[%s] sending and received error: %s",
					shorttxid(tctx.transactionSecContext.GetTxid()), err)
				return err
			}
		case tctxin := <-ws.Incomining:
			txid := tctxin.transactionSecContext.GetTxid()
			if tctx, ok := tctxs[txid]; ok {
				//remove existed tcx
				chaincodeLogger.Error("Get duplicated request for tx [%s] when we are handling [%s]",
					tctxin.transactionSecContext.GetTxid(), tctx.transactionSecContext.GetTxid())
				tctxin.failTx(fmt.Errorf("Duplicated transaction"))
				if tctxin.isTransaction {

				}
			} else if tctxin.isTransaction {
				//check duplicated
			}

			//generate message and send ...
			var msg *pb.ChaincodeMessage
			msg, err = createTransactionMessage(tctxin.transactionSecContext, tctxin.inputMsg)
			if err == nil {
				err = handler.setChaincodeSecurityContext(tctxin.transactionSecContext, msg)
			}

			if err == nil {
				tctx = tctxin
				chaincodeLogger.Debug("Accept new transaction handling [%s]", tctx.transactionSecContext.GetTxid())
				ws.resp <- msg
			} else {
				tctxin.failTx(err)
			}

		case <-handler.waitForKeepaliveTimer():
			if handler.chaincodeSupport.keepalive <= 0 {
				chaincodeLogger.Errorf("Invalid select: keepalive not on (keepalive=%d)", handler.chaincodeSupport.keepalive)
				continue
			}

			//TODO we could use this to hook into container lifecycle (kill the chaincode if not in use, etc)
			err = ws.Send(&pb.ChaincodeMessage{Type: pb.ChaincodeMessage_KEEPALIVE})
			if err != nil {
				err = fmt.Errorf("Error sending keepalive, err=%s", err)
				return
			} else {
				chaincodeLogger.Debug("Sent KEEPALIVE request")
			}
		}
	}
}

// Handler responsbile for management of Peer's side of chaincode stream
type Handler struct {
	sync.RWMutex
	ChatStream          ccintf.ChaincodeStream
	availableChatStream chan *workingStream
	FSM                 *fsm.FSM
	ChaincodeID         *pb.ChaincodeID

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

// createTransactionMessage creates a transaction message.
func createTransactionMessage(tx *pb.Transaction, cMsg *pb.ChaincodeInput) (*pb.ChaincodeMessage, error) {
	payload, err := proto.Marshal(cMsg)
	if err != nil {
		return nil, err
	}

	msgType := pb.ChaincodeMessage_QUERY
	switch tx.GetType() {
	case pb.Transaction_CHAINCODE_INVOKE:
		msgType = pb.ChaincodeMessage_TRANSACTION
	case pb.Transaction_CHAINCODE_DEPLOY:
		msgType = pb.ChaincodeMessage_INIT
	default:
	}

	return &pb.ChaincodeMessage{Type: msgType, Payload: payload, Txid: tx.GetTxid()}, nil
}

func (handler *Handler) afterStreamLeave(e *fsm.Event) error {

	// ws, ok := e.Args[0].(*workingStream)
	// if !ok {
	// 	panic("Not wrokingStream, wrong code")
	// }
	tctxs, ok := e.Args[1].(map[string]*transactionContext)
	if !ok {
		panic("Not tctxs, wrong code")
	}

	//fail all of the contexts
	for txid, tctx := range tctxs {
		tctx.responseNotifier <- &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_ERROR,
			Payload: []byte("Stream Failure"), Txid: txid}
	}

}

func (handler *Handler) serialSend(msg *pb.ChaincodeMessage) error {
	handler.serialLock.Lock()
	defer handler.serialLock.Unlock()
	if err := handler.ChatStream.Send(msg); err != nil {
		chaincodeLogger.Errorf("Error sending %s: %s", msg.Type.String(), err)
		return fmt.Errorf("Error sending %s: %s", msg.Type.String(), err)
	}
	return nil
}

func (handler *Handler) createTxContext(tx *pb.Transaction) (*transactionContext, error) {
	txid := tx.GetTxid()
	if handler.txCtxs == nil {
		return nil, fmt.Errorf("cannot create notifier for txid:%s", txid)
	}
	handler.Lock()
	defer handler.Unlock()
	if handler.txCtxs[txid] != nil {
		return nil, fmt.Errorf("txid:%s exists", txid)
	}
	txctx := &transactionContext{transactionSecContext: tx,
		responseNotifier:      make(chan *pb.ChaincodeMessage, 1),
		isTransaction:         tx.GetType() != pb.Transaction_CHAINCODE_QUERY,
		rangeQueryIteratorMap: make(map[string]statemgmt.RangeScanIterator)}
	handler.txCtxs[txid] = txctx
	return txctx, nil
}

func (handler *Handler) getTxContext(txid string) *transactionContext {
	handler.RLock()
	defer handler.RUnlock()
	return handler.txCtxs[txid]
}

//THIS CAN BE REMOVED ONCE WE SUPPORT CONFIDENTIALITY WITH CC-CALLING-CC
//we dissallow chaincode-chaincode interactions till confidentiality implications are understood
func (handler *Handler) canCallChaincode(tctx *transactionContext) error {
	secHelper := handler.chaincodeSupport.getSecHelper()
	if secHelper == nil {
		return nil
	}

	if txctx == nil {
		return fmt.Errorf("[%s]Error no context while checking for confidentiality. Sending %s", shorttxid(txid), pb.ChaincodeMessage_ERROR)
	} else if txctx.transactionSecContext == nil {
		return fmt.Errorf("[%s]Error transaction context is nil while checking for confidentiality. Sending %s", shorttxid(txid), pb.ChaincodeMessage_ERROR)
	} else if txctx.transactionSecContext.ConfidentialityLevel != pb.ConfidentialityLevel_PUBLIC {
		return fmt.Errorf("[%s]Error chaincode-chaincode interactions not supported for with privacy enabled. Sending %s", shorttxid(txid), pb.ChaincodeMessage_ERROR)
	}

	//not CONFIDENTIAL transaction, OK to call CC
	return nil
}

func (handler *Handler) encryptOrDecrypt(encrypt bool, tctx *transactionContext, payload []byte) ([]byte, error) {
	secHelper := handler.chaincodeSupport.getSecHelper()
	if secHelper == nil {
		return payload, nil
	}

	// TODO: this must be removed
	if txctx.transactionSecContext.ConfidentialityLevel == pb.ConfidentialityLevel_PUBLIC {
		return payload, nil
	}

	var enc crypto.StateEncryptor
	var err error
	if txctx.transactionSecContext.Type == pb.Transaction_CHAINCODE_DEPLOY {
		if enc, err = secHelper.GetStateEncryptor(handler.deployTXSecContext, handler.deployTXSecContext); err != nil {
			return nil, fmt.Errorf("error getting crypto encryptor for deploy tx :%s", err)
		}
	} else if txctx.transactionSecContext.Type == pb.Transaction_CHAINCODE_INVOKE || txctx.transactionSecContext.Type == pb.Transaction_CHAINCODE_QUERY {
		if enc, err = secHelper.GetStateEncryptor(handler.deployTXSecContext, txctx.transactionSecContext); err != nil {
			return nil, fmt.Errorf("error getting crypto encryptor %s", err)
		}
	} else {
		return nil, fmt.Errorf("invalid transaction type %s", txctx.transactionSecContext.Type.String())
	}
	if enc == nil {
		return nil, fmt.Errorf("secure context returns nil encryptor for tx %s", txid)
	}
	if chaincodeLogger.IsEnabledFor(logging.DEBUG) {
		chaincodeLogger.Debugf("[%s]Payload before encrypt/decrypt: %v", shorttxid(txid), payload)
	}
	if encrypt {
		payload, err = enc.Encrypt(payload)
	} else {
		payload, err = enc.Decrypt(payload)
	}
	if chaincodeLogger.IsEnabledFor(logging.DEBUG) {
		chaincodeLogger.Debugf("[%s]Payload after encrypt/decrypt: %v", shorttxid(txid), payload)
	}

	return payload, err
}

func (handler *Handler) decrypt(tctx *transactionContext, payload []byte) ([]byte, error) {
	return handler.encryptOrDecrypt(false, tctx, payload)
}

func (handler *Handler) encrypt(tctx *transactionContext, payload []byte) ([]byte, error) {
	return handler.encryptOrDecrypt(true, tctx, payload)
}

func (handler *Handler) getSecurityBinding(tx *pb.Transaction) ([]byte, error) {
	secHelper := handler.chaincodeSupport.getSecHelper()
	if secHelper == nil {
		return nil, nil
	}

	return secHelper.GetTransactionBinding(tx)
}

func (handler *Handler) deregister() {

	// clean up rangeQueryIteratorMap
	for _, context := range handler.txCtxs {
		for _, v := range context.rangeQueryIteratorMap {
			v.Close()
		}
	}

	handler.chaincodeSupport.deregisterHandler(handler)
}

func (handler *Handler) triggerNextState(msg *pb.ChaincodeMessage, send bool) {
	handler.nextState <- &nextStateInfo{msg, send}
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

	w := &workingStream{ChaincodeStream: peerChatStream}

	w.FSM = fsm.NewFSM(
		readystate,
		fsm.Events{
			{Name: pb.ChaincodeMessage_INIT.String(), Src: []string{readystate}, Dst: transactionstate},
			{Name: pb.ChaincodeMessage_TRANSACTION.String(), Src: []string{readystate}, Dst: transactionstate},
			{Name: pb.ChaincodeMessage_QUERY.String(), Src: []string{readystate}, Dst: transactionstate},
			//so in fact we have two different routine
			{Name: pb.ChaincodeMessage_ERROR.String(), Src: []string{busyxactstate}, Dst: busyxactstate},
			{Name: pb.ChaincodeMessage_ERROR.String(), Src: []string{busystate}, Dst: transactionstate},
			{Name: pb.ChaincodeMessage_RESPONSE.String(), Src: []string{busyxactstate}, Dst: busyxactstate},
			{Name: pb.ChaincodeMessage_RESPONSE.String(), Src: []string{busystate}, Dst: transactionstate},
			{Name: cc_message_read, Src: []string{transactionstate}, Dst: busystate},
			{Name: cc_message_readmore, Src: []string{transactionstate, busystate}, Dst: busyxactstate},
			{Name: cc_message_readend, Src: []string{busyxactstate}, Dst: busystate},
			{Name: cc_message_write, Src: []string{transactionstate}, Dst: busystate},
			{Name: cc_message_finish, Src: []string{transactionstate, busyxactstate, busystate}, Dst: readystate},
			// now we just filter the keepalive messsage out
			// {Name: cc_message_keepalive, Src: []string{transactionstate}, Dst: transactionstate},
			// {Name: cc_message_keepalive, Src: []string{readystate}, Dst: readystate},
			// {Name: cc_message_keepalive, Src: []string{busyxactstate}, Dst: busyxactstate},
			// {Name: cc_message_keepalive, Src: []string{busystate}, Dst: busystate},
		},
		fsm.Callbacks{
			"after_" + cc_message_read:     func(e *fsm.Event) { handler.afterGetState(e, w) },
			"after_" + cc_message_readmore: func(e *fsm.Event) { handler.afterRangeQueryState(e, w) },
			"before_" + cc_message_write:   func(e *fsm.Event) { handler.beforeCompletedEvent(e, v.FSM.Current()) },
			"after_" + cc_message_write:    func(e *fsm.Event) { handler.afterPutState(e, w) },
			"leave_" + readystate:          func(e *fsm.Event) { handler.beforeRegisterEvent(e, v.FSM.Current()) },
			"enter_" + readystate:          func(e *fsm.Event) { handler.beforeRegisterEvent(e, v.FSM.Current()) },
		},
	)
}

func getStreamMsgType(ccMsg *pb.ChaincodeMessage) (customType string) {

	switch ccMsg.Type {
	case pb.ChaincodeMessage_QUERY_COMPLETED,
		pb.ChaincodeMessage_QUERY_ERROR,
		pb.ChaincodeMessage_COMPLETED:
		customType = cc_message_finish
	default:
		customType = cc_message_busy
	}

	return
}

func newChaincodeSupportHandler(chaincodeSupport *ChaincodeSupport, peerChatStream ccintf.ChaincodeStream) *Handler {
	v := &Handler{
		ChatStream: peerChatStream,
	}
	v.txCtxs = make(map[string]*transactionContext)
	v.chaincodeSupport = chaincodeSupport
	//we want this to block
	v.nextState = make(chan *nextStateInfo)

	v.FSM = fsm.NewFSM(
		establishedstate,
		fsm.Events{
			//Send REGISTERED, then, if deploy { trigger INIT(via INIT) } else { trigger READY(via COMPLETED) }
			{Name: cc_op_init, Src: []string{establishedstate}, Dst: initstate},
			{Name: cc_transaction_done, Src: []string{initstate, readystate}, Dst: readystate},
			{Name: cc_transaction_fail, Src: []string{initstate}, Dst: endstate},
			{Name: cc_transaction_fail, Src: []string{readystate}, Dst: readystate},
			{Name: cc_stream_join, Src: []string{readystate}, Dst: readystate},
			{Name: cc_stream_leave, Src: []string{readystate}, Dst: readystate},
		},
		fsm.Callbacks{
		// "before_" + pb.ChaincodeMessage_REGISTER.String():               func(e *fsm.Event) { v.beforeRegisterEvent(e, v.FSM.Current()) },
		// "before_" + pb.ChaincodeMessage_COMPLETED.String():              func(e *fsm.Event) { v.beforeCompletedEvent(e, v.FSM.Current()) },
		// "before_" + pb.ChaincodeMessage_INIT.String():                   func(e *fsm.Event) { v.beforeInitState(e, v.FSM.Current()) },
		// "after_" + pb.ChaincodeMessage_GET_STATE.String():               func(e *fsm.Event) { v.afterGetState(e, v.FSM.Current()) },
		// "after_" + pb.ChaincodeMessage_RANGE_QUERY_STATE.String():       func(e *fsm.Event) { v.afterRangeQueryState(e, v.FSM.Current()) },
		// "after_" + pb.ChaincodeMessage_RANGE_QUERY_STATE_NEXT.String():  func(e *fsm.Event) { v.afterRangeQueryStateNext(e, v.FSM.Current()) },
		// "after_" + pb.ChaincodeMessage_RANGE_QUERY_STATE_CLOSE.String(): func(e *fsm.Event) { v.afterRangeQueryStateClose(e, v.FSM.Current()) },
		// "after_" + pb.ChaincodeMessage_PUT_STATE.String():               func(e *fsm.Event) { v.afterPutState(e, v.FSM.Current()) },
		// "after_" + pb.ChaincodeMessage_DEL_STATE.String():               func(e *fsm.Event) { v.afterDelState(e, v.FSM.Current()) },
		// "after_" + pb.ChaincodeMessage_INVOKE_CHAINCODE.String():        func(e *fsm.Event) { v.afterInvokeChaincode(e, v.FSM.Current()) },
		// "enter_" + establishedstate:                                     func(e *fsm.Event) { v.enterEstablishedState(e, v.FSM.Current()) },
		// "enter_" + initstate:                                            func(e *fsm.Event) { v.enterInitState(e, v.FSM.Current()) },
		// "enter_" + readystate:                                           func(e *fsm.Event) { v.enterReadyState(e, v.FSM.Current()) },
		// "enter_" + busyinitstate:                                        func(e *fsm.Event) { v.enterBusyState(e, v.FSM.Current()) },
		// "enter_" + busyxactstate:                                        func(e *fsm.Event) { v.enterBusyState(e, v.FSM.Current()) },
		// "enter_" + endstate:                                             func(e *fsm.Event) { v.enterEndState(e, v.FSM.Current()) },
		},
	)

	return v
}

func (handler *Handler) onWorkStreamJoin(e *fsm.Event) error {

}

func (handler *Handler) onWorkStreamLeave(e *fsm.Event) {

	//	ws := e.Args[0].(*workingStream)
}

func (handler *Handler) notifyDuringStartup(val bool) {
	//if USER_RUNS_CC readyNotify will be nil
	if handler.readyNotify != nil {
		chaincodeLogger.Debug("Notifying during startup")
		handler.readyNotify <- val
	} else {
		chaincodeLogger.Debug("nothing to notify (dev mode ?)")
	}
}

// beforeRegisterEvent is invoked when chaincode tries to register.
func (handler *Handler) beforeRegisterEvent(e *fsm.Event, state string) {
	chaincodeLogger.Debugf("Received %s in state %s", e.Event, state)
	msg, ok := e.Args[0].(*pb.ChaincodeMessage)
	if !ok {
		e.Cancel(fmt.Errorf("Received unexpected message type"))
		return
	}
	chaincodeID := &pb.ChaincodeID{}
	err := proto.Unmarshal(msg.Payload, chaincodeID)
	if err != nil {
		e.Cancel(fmt.Errorf("Error in received %s, could NOT unmarshal registration info: %s", pb.ChaincodeMessage_REGISTER, err))
		return
	}

	// Now register with the chaincodeSupport
	handler.ChaincodeID = chaincodeID
	err = handler.chaincodeSupport.registerHandler(handler)
	if err != nil {
		e.Cancel(err)
		handler.notifyDuringStartup(false)
		return
	}

	chaincodeLogger.Debugf("Got %s for chaincodeID = %s, sending back %s", e.Event, chaincodeID, pb.ChaincodeMessage_REGISTERED)
	if err := handler.serialSend(&pb.ChaincodeMessage{Type: pb.ChaincodeMessage_REGISTERED}); err != nil {
		e.Cancel(fmt.Errorf("Error sending %s: %s", pb.ChaincodeMessage_REGISTERED, err))
		handler.notifyDuringStartup(false)
		return
	}
}

func (handler *Handler) notify(msg *pb.ChaincodeMessage) {
	handler.Lock()
	defer handler.Unlock()
	tctx := handler.txCtxs[msg.Txid]
	if tctx == nil {
		chaincodeLogger.Debugf("notifier Txid:%s does not exist", msg.Txid)
	} else {
		chaincodeLogger.Debugf("notifying Txid:%s", msg.Txid)
		tctx.responseNotifier <- msg

		// clean up rangeQueryIteratorMap
		for _, v := range tctx.rangeQueryIteratorMap {
			v.Close()
		}
	}
}

// beforeCompletedEvent is invoked when chaincode has completed execution of init, invoke or query.
func (handler *Handler) beforeCompletedEvent(e *fsm.Event, state string) {
	msg, ok := e.Args[0].(*pb.ChaincodeMessage)
	if !ok {
		e.Cancel(fmt.Errorf("Received unexpected message type"))
		return
	}
	// Notify on channel once into READY state
	chaincodeLogger.Debugf("[%s]beforeCompleted - not in ready state will notify when in readystate", shorttxid(msg.Txid))
	return
}

// beforeInitState is invoked before an init message is sent to the chaincode.
func (handler *Handler) beforeInitState(e *fsm.Event, state string) {
	chaincodeLogger.Debugf("Before state %s.. notifying waiter that we are up", state)
	handler.notifyDuringStartup(true)
}

func (handler *Handler) handleGetState(ledgerObj *ledger.Ledger, msg *pb.ChaincodeMessage, tctx *transactionContext) (*pb.ChaincodeMessage, error) {

	var serialSendMsg *pb.ChaincodeMessage
	key := string(msg.Payload)

	// Invoke ledger to get state
	chaincodeID := handler.ChaincodeID.Name
	readCommittedState := !tctx.isTransaction
	res, err := ledgerObj.GetState(chaincodeID, key, readCommittedState)
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
		if res, err = handler.decrypt(msg.Txid, res); err == nil {
			// Send response msg back to chaincode. GetState will not trigger event
			chaincodeLogger.Debugf("[%s]Got state. Sending %s", shorttxid(msg.Txid), pb.ChaincodeMessage_RESPONSE)
			return &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_RESPONSE, Payload: res, Txid: msg.Txid}
		} else {
			// Send err msg back to chaincode.
			chaincodeLogger.Errorf("[%s]Got error (%s) while decrypting. Sending %s", shorttxid(msg.Txid), err, pb.ChaincodeMessage_ERROR)
			return err
		}

	}

}

const maxRangeQueryStateLimit = 100

// Handles query to ledger to rage query state next
func (handler *Handler) handleRangeQuery(rangeIter statemgmt.RangeScanIterator, string iterID, tctx *transactionContext) (*pb.ChaincodeMessage, error, bool) {

	var keysAndValues []*pb.RangeQueryStateKeyValue
	var i = uint32(0)
	hasNext := true
	txid := tctx.transactionSecContext.GetTxid()
	for ; hasNext && i < maxRangeQueryStateLimit; i++ {
		key, value := rangeIter.GetKeyValue()
		// Decrypt the data if the confidential is enabled
		decryptedValue, decryptErr := handler.decrypt(tctx, value)
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
func (handler *Handler) handleRangeQueryState(ledgerObj *ledger.Ledger, msg *pb.ChaincodeMessage, tctx *transactionContext) (*pb.ChaincodeMessage, error) {

	rangeQueryState := &pb.RangeQueryState{}
	unmarshalErr := proto.Unmarshal(msg.Payload, rangeQueryState)
	if unmarshalErr != nil {
		chaincodeLogger.Errorf("Failed to unmarshall range query request. Sending %s", pb.ChaincodeMessage_ERROR)
		return nil, unmarshalErr
	}

	chaincodeID := handler.ChaincodeID.Name

	readCommittedState := !tctx.isTransaction
	rangeIter, err := ledgerObj.GetStateRangeScanIterator(chaincodeID, rangeQueryState.StartKey, rangeQueryState.EndKey, readCommittedState)
	if err != nil {
		// Send error msg back to chaincode. GetState will not trigger event
		chaincodeLogger.Errorf("Failed to get ledger scan iterator. Sending %s", pb.ChaincodeMessage_ERROR)
		return nil, err
	}

	iterID := util.GenerateUUID()
	tctx.responseNotifier[iterID] = rangeIter

	ret, err, hasNext := handler.handleRangeQuery(rangeIter, iterID, tctx)
	if !hasNext {
		rangeIter.Close()
		delete(tctx.responseNotifier, iterID)
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

	rangeIter := tctx.responseNotifier[rangeQueryStateNext.ID]

	if rangeIter == nil {
		return fmt.Errorf("Range query iterator not found")
	}

	ret, err, hasNext := handler.handleRangeQuery(rangeIter, rangeQueryStateNext.ID, tctx)
	if !hasNext {
		rangeIter.Close()
		delete(tctx.responseNotifier, rangeQueryStateNext.ID)
	}

	return ret, err

}

// Handles the closing of a state iterator
func (handler *Handler) handleRangeQueryStateClose(msg *pb.ChaincodeMessage, tctx *transactionContext) (*pb.ChaincodeMessage, error) {

	rangeQueryStateClose := &pb.RangeQueryStateClose{}
	unmarshalErr := proto.Unmarshal(msg.Payload, rangeQueryStateClose)
	if unmarshalErr != nil {
		chaincodeLogger.Errorf("Failed to unmarshall state range query close request. Sending %s", pb.ChaincodeMessage_ERROR)
		return unmarshalErr
	}

	iter := tctx.responseNotifier[rangeQueryStateClose.ID]
	if iter != nil {
		iter.Close()
		delete(tctx.responseNotifier, rangeQueryStateClose.ID)
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

func (handler *Handler) handlePutState(ledgerObj *ledger.Ledger, msg *pb.ChaincodeMessage, tctx *transactionContext) (*pb.ChaincodeMessage, error) {

	chaincodeID := handler.ChaincodeID.Name
	var err error

	if msg.Type.String() == pb.ChaincodeMessage_PUT_STATE.String() {
		putStateInfo := &pb.PutStateInfo{}
		unmarshalErr := proto.Unmarshal(msg.Payload, putStateInfo)
		if unmarshalErr != nil {
			chaincodeLogger.Debugf("[%s]Unable to decipher payload. Sending %s", shorttxid(msg.Txid), pb.ChaincodeMessage_ERROR)
			return nil, unmarshalErr
		}

		var pVal []byte
		// Encrypt the data if the confidential is enabled
		if pVal, err = handler.encrypt(msg.Txid, putStateInfo.Value); err == nil {
			// Invoke ledger to put state
			err = ledgerObj.SetState(chaincodeID, putStateInfo.Key, pVal)
		}
	} else if msg.Type.String() == pb.ChaincodeMessage_DEL_STATE.String() {
		// Invoke ledger to delete state
		key := string(msg.Payload)
		err = ledgerObj.DeleteState(chaincodeID, key)
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

func (handler *Handler) handleInvokeChaincode(ledgerObj *ledger.Ledger, msg *pb.ChaincodeMessage, tctx *transactionContext) (*pb.ChaincodeMessage, error) {

	if err := handler.canCallChaincode(tctx); err != nil {
		return nil, err
	}
	chaincodeSpec := &pb.ChaincodeSpec{}
	unmarshalErr := proto.Unmarshal(msg.Payload, chaincodeSpec)
	if unmarshalErr != nil {
		chaincodeLogger.Debugf("[%s]Unable to decipher payload. Sending %s", shorttxid(msg.Txid), pb.ChaincodeMessage_ERROR)
		return nil, unmarshalErr
	}

	// Get the chaincodeID to invoke
	newChaincodeID := chaincodeSpec.ChaincodeID.Name

	// Create the transaction object
	chaincodeInvocationSpec := &pb.ChaincodeInvocationSpec{ChaincodeSpec: chaincodeSpec}
	var txtype pb.Transaction_Type
	if msg.Type == ChaincodeMessage_INVOKE_CHAINCODE {
		txtype = pb.Transaction_CHAINCODE_INVOKE
	} else {
		if msg.Type != pb.ChaincodeMessage_INVOKE_QUERY {
			panic(fmt.Error("Impossible message type", msg.Type))
		}
		txtype = pb.Transaction_CHAINCODE_QUERY
	}
	transaction, _ := pb.NewChaincodeExecute(chaincodeInvocationSpec, msg.Txid, txtype)

	// Launch the new chaincode if not already running
	_, chaincodeInput, launchErr := handler.chaincodeSupport.Launch(context.Background(), transaction)
	if launchErr != nil {
		chaincodeLogger.Debugf("[%s]Failed to launch invoked chaincode. Sending %s", shorttxid(msg.Txid), pb.ChaincodeMessage_ERROR)
		//triggerNextStateMsg = &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_ERROR, Payload: payload, Txid: msg.Txid}
		return nil, launchErr
	}

	// Execute the chaincode
	//NOTE: when confidential C-call-C is understood, transaction should have the correct sec context for enc/dec
	response, execErr := handler.chaincodeSupport.Execute(context.Background(), newChaincodeID, chaincodeInput, transaction)

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

func (handler *Handler) enterEstablishedState(e *fsm.Event, state string) {
	handler.notifyDuringStartup(true)
}

func (handler *Handler) enterInitState(e *fsm.Event, state string) {
	ccMsg, ok := e.Args[0].(*pb.ChaincodeMessage)
	if !ok {
		e.Cancel(fmt.Errorf("Received unexpected message type"))
		return
	}
	chaincodeLogger.Debugf("[%s]Entered state %s", shorttxid(ccMsg.Txid), state)
	//very first time entering init state from established, send message to chaincode
	if ccMsg.Type == pb.ChaincodeMessage_INIT {
		// Mark isTransaction to allow put/del state and invoke other chaincodes
		handler.markIsTransaction(ccMsg.Txid, true)
		if err := handler.serialSend(ccMsg); err != nil {
			errMsg := &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_ERROR, Payload: []byte(fmt.Sprintf("Error sending %s: %s", pb.ChaincodeMessage_INIT, err)), Txid: ccMsg.Txid}
			handler.notify(errMsg)
		}
	}
}

func (handler *Handler) enterReadyState(e *fsm.Event, state string) {
	// Now notify
	msg, ok := e.Args[0].(*pb.ChaincodeMessage)
	//we have to encrypt chaincode event payload. We cannot encrypt event type as
	//it is needed by the event system to filter clients by
	if ok && msg.ChaincodeEvent != nil && msg.ChaincodeEvent.Payload != nil {
		var err error
		if msg.Payload, err = handler.encrypt(msg.Txid, msg.Payload); nil != err {
			chaincodeLogger.Errorf("[%s]Failed to encrypt chaincode event payload", msg.Txid)
			msg.Payload = []byte(fmt.Sprintf("Failed to encrypt chaincode event payload %s", err.Error()))
			msg.Type = pb.ChaincodeMessage_ERROR
		}
	}
	handler.deleteIsTransaction(msg.Txid)
	if !ok {
		e.Cancel(fmt.Errorf("Received unexpected message type"))
		return
	}
	chaincodeLogger.Debugf("[%s]Entered state %s", shorttxid(msg.Txid), state)
	handler.notify(msg)
}

func (handler *Handler) enterEndState(e *fsm.Event, state string) {
	// Now notify
	msg, ok := e.Args[0].(*pb.ChaincodeMessage)
	handler.deleteIsTransaction(msg.Txid)
	if !ok {
		e.Cancel(fmt.Errorf("Received unexpected message type"))
		return
	}
	chaincodeLogger.Debugf("[%s]Entered state %s", shorttxid(msg.Txid), state)
	handler.notify(msg)
	e.Cancel(fmt.Errorf("Entered end state"))
}

func (handler *Handler) cloneTx(tx *pb.Transaction) (*pb.Transaction, error) {
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

func (handler *Handler) initializeSecContext(tx, depTx *pb.Transaction) error {
	//set deploy transaction on the handler
	if depTx != nil {
		//we are given a deep clone of depTx.. Just use it
		handler.deployTXSecContext = depTx
	} else {
		//nil depTx => tx is a deploy transaction, clone it
		var err error
		handler.deployTXSecContext, err = handler.cloneTx(tx)
		if err != nil {
			return fmt.Errorf("Failed to clone transaction: %s\n", err)
		}
	}

	//don't need the payload which is not useful and rather large
	handler.deployTXSecContext.Payload = nil

	//we need to null out path from depTx as invoke or queries don't have it
	cID := &pb.ChaincodeID{}
	err := proto.Unmarshal(handler.deployTXSecContext.ChaincodeID, cID)
	if err != nil {
		return fmt.Errorf("Failed to unmarshall : %s\n", err)
	}

	cID.Path = ""
	data, err := proto.Marshal(cID)
	if err != nil {
		return fmt.Errorf("Failed to marshall : %s\n", err)
	}

	handler.deployTXSecContext.ChaincodeID = data

	return nil
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

//if initArgs is set (should be for "deploy" only) move to Init
//else move to ready
func (handler *Handler) initOrReady(initArgs [][]byte, tx *pb.Transaction, depTx *pb.Transaction) (chan *pb.ChaincodeMessage, error) {
	var ccMsg *pb.ChaincodeMessage
	var send bool

	txctx, funcErr := handler.createTxContext(tx)
	if funcErr != nil {
		return nil, funcErr
	}

	txid := tx.GetTxid()
	notfy := txctx.responseNotifier

	if initArgs != nil {
		chaincodeLogger.Debug("sending INIT")
		ccMsg, funcErr := createTransactionMessage(tx, &pb.ChaincodeInput{Args: initArgs})
		if funcErr != nil {
			return nil, fmt.Errorf("Failed to marshall %s : %s\n", ccMsg.Type.String(), funcErr)
		}
		send = false
	} else {
		chaincodeLogger.Debug("sending READY")
		ccMsg = &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_READY, Txid: txid}
		send = true
	}

	if err := handler.initializeSecContext(tx, depTx); err != nil {
		return nil, err
	}

	//if security is disabled the context elements will just be nil
	if err := handler.setChaincodeSecurityContext(tx, ccMsg); err != nil {
		return nil, err
	}

	handler.triggerNextState(ccMsg, send)

	return notfy, nil
}

// HandleMessage implementation of MessageHandler interface.  Peer's handling of Chaincode messages.
func (handler *Handler) HandleMessage(msg *pb.ChaincodeMessage) error {
	chaincodeLogger.Debugf("[%s]Handling ChaincodeMessage of type: %s in state %s", shorttxid(msg.Txid), msg.Type, handler.FSM.Current())

	//QUERY_COMPLETED message can happen ONLY for Transaction_QUERY (stateless)
	if msg.Type == pb.ChaincodeMessage_QUERY_COMPLETED {
		chaincodeLogger.Debugf("[%s]HandleMessage- QUERY_COMPLETED. Notify", msg.Txid)
		handler.deleteIsTransaction(msg.Txid)
		var err error
		if msg.Payload, err = handler.encrypt(msg.Txid, msg.Payload); nil != err {
			chaincodeLogger.Errorf("[%s]Failed to encrypt query result %s", msg.Txid, string(msg.Payload))
			msg.Payload = []byte(fmt.Sprintf("Failed to encrypt query result %s", err.Error()))
			msg.Type = pb.ChaincodeMessage_QUERY_ERROR
		}
		handler.notify(msg)
		return nil
	} else if msg.Type == pb.ChaincodeMessage_QUERY_ERROR {
		chaincodeLogger.Debugf("[%s]HandleMessage- QUERY_ERROR (%s). Notify", msg.Txid, string(msg.Payload))
		handler.deleteIsTransaction(msg.Txid)
		handler.notify(msg)
		return nil
	} else if msg.Type == pb.ChaincodeMessage_INVOKE_QUERY {
		// Received request to query another chaincode from shim
		chaincodeLogger.Debugf("[%s]HandleMessage- Received request to query another chaincode", msg.Txid)
		handler.handleQueryChaincode(msg)
		return nil
	}
	if handler.FSM.Cannot(msg.Type.String()) {
		// Check if this is a request from validator in query context
		if msg.Type == pb.ChaincodeMessage_PUT_STATE || msg.Type == pb.ChaincodeMessage_DEL_STATE || msg.Type == pb.ChaincodeMessage_INVOKE_CHAINCODE {
			// Check if this TXID is a transaction
			if !handler.getIsTransaction(msg.Txid) {
				payload := []byte(fmt.Sprintf("[%s]Cannot handle %s in query context", msg.Txid, msg.Type.String()))
				chaincodeLogger.Errorf("[%s]Cannot handle %s in query context. Sending %s", msg.Txid, msg.Type.String(), pb.ChaincodeMessage_ERROR)
				errMsg := &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_ERROR, Payload: payload, Txid: msg.Txid}
				handler.serialSend(errMsg)
				return fmt.Errorf("Cannot handle %s in query context", msg.Type.String())
			}
		}

		// Other errors
		return fmt.Errorf("[%s]Chaincode handler validator FSM cannot handle message (%s) with payload size (%d) while in state: %s", msg.Txid, msg.Type.String(), len(msg.Payload), handler.FSM.Current())
	}
	eventErr := handler.FSM.Event(msg.Type.String(), msg)
	filteredErr := filterError(eventErr)
	if filteredErr != nil {
		chaincodeLogger.Errorf("[%s]Failed to trigger FSM event %s: %s", msg.Txid, msg.Type.String(), filteredErr)
	}

	return filteredErr
}

// Filter the Errors to allow NoTransitionError and CanceledError to not propagate for cases where embedded Err == nil
func filterError(errFromFSMEvent error) error {
	if errFromFSMEvent != nil {
		if noTransitionErr, ok := errFromFSMEvent.(*fsm.NoTransitionError); ok {
			if noTransitionErr.Err != nil {
				// Squash the NoTransitionError
				return errFromFSMEvent
			}
			chaincodeLogger.Debugf("Ignoring NoTransitionError: %s", noTransitionErr)
		}
		if canceledErr, ok := errFromFSMEvent.(*fsm.CanceledError); ok {
			if canceledErr.Err != nil {
				// Squash the CanceledError
				return canceledErr
			}
			chaincodeLogger.Debugf("Ignoring CanceledError: %s", canceledErr)
		}
	}
	return nil
}

func (handler *Handler) sendExecuteMessage(cMsg *pb.ChaincodeInput, tx *pb.Transaction) (chan *pb.ChaincodeMessage, error) {
	txctx, err := handler.createTxContext(tx)
	if err != nil {
		return nil, err
	}

	msg, err := createTransactionMessage(tx, cMsg)
	if err != nil {
		return nil, fmt.Errorf("Failed to transaction message(%s)", err)
	}

	isTransaction := tx.GetType() == pb.Transaction_CHAINCODE_INVOKE

	// Mark TXID as either transaction or query
	chaincodeLogger.Debugf("[%s]Inside sendExecuteMessage. Message %s", shorttxid(msg.Txid), msg.Type.String())
	handler.markIsTransaction(msg.Txid, isTransaction)

	//if security is disabled the context elements will just be nil
	if err := handler.setChaincodeSecurityContext(tx, msg); err != nil {
		return nil, err
	}

	// Trigger FSM event if it is a transaction
	if isTransaction {
		chaincodeLogger.Debugf("[%s]sendExecuteMsg trigger event %s", shorttxid(msg.Txid), msg.Type)
		handler.triggerNextState(msg, true)
	} else {
		// Send the message to shim
		chaincodeLogger.Debugf("[%s]sending query", shorttxid(msg.Txid))
		if err = handler.serialSend(msg); err != nil {
			return nil, fmt.Errorf("[%s]SendMessage error sending (%s)", shorttxid(msg.Txid), err)
		}
	}

	return txctx.responseNotifier, nil
}

func (handler *Handler) isRunning() bool {
	switch handler.FSM.Current() {
	case createdstate:
		fallthrough
	case establishedstate:
		fallthrough
	case initstate:
		return false
	default:
		return true
	}
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
