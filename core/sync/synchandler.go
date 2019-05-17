package sync

import (
	"fmt"
	"github.com/abchain/fabric/core/ledger"
	pb "github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
	"sync"
	"time"
)

//some utilities when running for an session
type sessionRT struct {
	ctx  context.Context
	core *coreHandlerAdapter
	opts *syncOpt

	tag        string
	endSession func()
	endRequest context.CancelFunc
}

func newSessionRT(root *syncHandler, endF func()) *sessionRT {

	return &sessionRT{
		ctx:        root.handlerCtx,
		core:       root.core,
		opts:       root.opts,
		endSession: endF,
		tag:        root.remotePeerId.GetName(),
	}
}

//standard implement for idleTimeout method in ISessionHandler
func (rt *sessionRT) idleTimeout() time.Duration {
	return time.Second * time.Duration(rt.opts.baseIdleTimeout)
}

//standard implement for onClose method in ISessionHandler
func (rt *sessionRT) onClose() {
	logger.Infof("current session [%s] is notified closed", rt.tag)
	if rt.endRequest != nil {
		rt.endRequest()
	}

	if rt.endSession != nil {
		rt.endSession()
	}
}

//return a  waiting function for be able to write, priority for the done of ctx
func (rt *sessionRT) RequestNew() func() error {

	if rt.endRequest != nil {
		rt.endRequest()
	}

	wctx, cf := context.WithCancel(rt.ctx)
	rt.endRequest = cf

	return func() error {
		select {
		case <-wctx.Done():
			return wctx.Err()
		default:
			select {
			case <-wctx.Done():
				return wctx.Err()
			case <-rt.core.SessionCanWrite():
				return nil
			}
		}
	}
}

type SyncMsgPrefilter interface {
	ForSimple(*pb.SimpleReq) bool
	ForSession(*pb.OpenSession) bool
}

type dummyPreFilter bool

func (b dummyPreFilter) ForSimple(*pb.SimpleReq) bool    { return bool(b) }
func (b dummyPreFilter) ForSession(*pb.OpenSession) bool { return bool(b) }

type syncHandler struct {
	core         *coreHandlerAdapter
	remotePeerId *pb.PeerID
	opts         *syncOpt
	//	streamStub      *pb.StreamStub
	localLedger     *ledger.Ledger
	handlerCtx      context.Context
	stopMainRoutine context.CancelFunc

	rDataLock         sync.RWMutex
	remoteLedgerState *pb.LedgerState
}

func newSyncHandler(ctx context.Context, remoterId *pb.PeerID,
	l *ledger.Ledger, opts *syncOpt) *syncHandler {
	logger.Debug("create handler for peer", remoterId)
	hctx, hf := context.WithCancel(ctx)

	h := &syncHandler{
		remotePeerId: remoterId,
		//		streamStub:   sstub,
		localLedger:     l,
		opts:            opts,
		core:            newHandlerAdapter(),
		handlerCtx:      hctx,
		stopMainRoutine: hf,
	}

	h.core.sessionOpt.maxWindow = opts.baseSessionWindow
	h.core.sessionOpt.maxPending = opts.basePendingReqLimit
	go h.core.handlerRoutine(hctx, h)

	return h
}

func (h *syncHandler) PushLocalLedgerState(strm *pb.StreamHandler, s *pb.LedgerState) error {
	return h.core.Push(strm, &pb.SimpleResp{Resp: &pb.SimpleResp_State{State: s}})
}

func (h *syncHandler) GetRemoteLedgerState() *pb.LedgerState {
	h.rDataLock.RLock()
	defer h.rDataLock.RUnlock()
	return h.remoteLedgerState
}

func (h *syncHandler) Stop() {

	h.stopMainRoutine()
	logger.Debugf("handler for session [%v] is stopped", h.remotePeerId)

}

func (h *syncHandler) Tag() string               { return "syncing" }
func (h *syncHandler) EnableLoss() bool          { return false }
func (h *syncHandler) NewMessage() proto.Message { return new(pb.SyncMsg) }
func (h *syncHandler) HandleMessage(s *pb.StreamHandler, m proto.Message) (err error) {

	defer func() {

		if ferr := recover(); ferr != nil {
			err = fmt.Errorf("Encounter fatal error in handling: %v", ferr)
		}

	}()

	h.core.syncCore.HandleMessage(m.(*pb.SyncMsg), s)
	return
}
func (h *syncHandler) BeforeSendMessage(proto.Message) error {
	return nil
}
func (h *syncHandler) OnWriteError(e error) {
	logger.Error("Sync handler encounter writer error:", e)
}

// func (syncHandler *stateSyncHandler) buildLocalLedgerStatus() error {

// 	blockHeight := syncHandler.ledger.GetBlockchainSize()

// 	localStatehash, err := syncHandler.ledger.GetCurrentStateHash()
// 	if err == nil {
// 		err = syncHandler.sendSyncMsg(nil, code, &pb.SyncStateResp{
// 			Statehash:   localStatehash,
// 			BlockHeight: blockHeight})
// 	}
// 	return err
// }

func (h *syncHandler) onPush(data *pb.SimpleResp) {
	if s := data.GetState(); s != nil {
		logger.Debugf("remote peer [%v] push state: %v", h.remotePeerId, s)
		h.rDataLock.Lock()
		defer h.rDataLock.Unlock()
		h.remoteLedgerState = s

	} else {
		logger.Infof("remote peer [%v] push unknown data: %v", h.remotePeerId, data)
	}
}

var handlerIsBusy = fmt.Errorf("service is busy")

func (h *syncHandler) onRequest(req *pb.SimpleReq) (*pb.SimpleResp, error) {

	if !h.opts.ForSimple(req) {
		return nil, handlerIsBusy
	}

	switch r := req.GetReq().(type) {
	case *pb.SimpleReq_State:
		h.rDataLock.Lock()
		logger.Debugf("remote peer [%v] push and request state: %v", h.remotePeerId, r.State)
		h.remoteLedgerState = r.State
		h.rDataLock.Unlock()
		return &pb.SimpleResp{
			Resp: &pb.SimpleResp_State{State: h.localLedger.GetLedgerStatus()},
		}, nil
	case *pb.SimpleReq_Tx:
		return &pb.SimpleResp{Resp: h.onRequestTx(r.Tx)}, nil
	default:
		return nil, fmt.Errorf("Unrecognized request")
	}
}

func (h *syncHandler) onSessionOpen(req *pb.OpenSession, resp *pb.AcceptSession) (ISessionHandler, error) {

	if !h.opts.ForSession(req) {
		return nil, handlerIsBusy
	}

	var ret ISessionHandler
	var err error

	switch r := req.GetFor().(type) {
	case *pb.OpenSession_Query:
		ret, err = newStateQueryHandler(h)
	case *pb.OpenSession_BlocksOrDelta:
		ret, err = newBlockSyncHandler(r.BlocksOrDelta, h)
	case *pb.OpenSession_Index:
		ret, err = newSyncIndexHandler(r.Index, h)
	case *pb.OpenSession_Fullstate:
		if ha, herr := newStateSyncHandler(r.Fullstate, h); herr != nil {
			err = herr
		} else {
			ha.FillAcceptMsg(resp)
			ret = ha
		}

	default:
		return nil, fmt.Errorf("Unrecognized request")
	}

	//fill the states of resp
	resp.States = h.localLedger.GetLedgerStatus()
	return ret, err
}

//handling for simple request on localledger ...
func (h *syncHandler) onRequestTx(tx *pb.TxQuery) *pb.SimpleResp_Tx {
	resp := &pb.SimpleResp_Tx{Tx: &pb.TransactionBlock{}}
	if ids := tx.GetTxid(); len(ids) > h.opts.txOption.maxSyncTxCount {
		//truncate request silencily
		resp.Tx.Transactions = h.localLedger.GetTransactionsByID(ids[:h.opts.txOption.maxSyncTxCount])
	} else {
		resp.Tx.Transactions = h.localLedger.GetTransactionsByID(ids)
	}
	return resp
}

// func (syncHandler *stateSyncHandler) handshake() error {
// 	return syncHandler.sendLedgerInfo(pb.SyncMsg_SYNC_QUERY_LEDGER)
// }

// func (syncHandler *stateSyncHandler) onRecvLedgerInfo(e *fsm.Event) error {

// 	syncStateResp := &pb.SyncStateResp{}
// 	syncMsg := syncHandler.onRecvSyncMsg(e, syncStateResp)

// 	var err error
// 	if syncMsg == nil {
// 		err = fmt.Errorf("unexpected sync message")
// 		e.Cancel(err)
// 	}

// 	logger.Infof("Remote peer[%s]: block height<%d>, statehash<%x>. <%s>", syncHandler.remotePeerId,
// 		syncStateResp.BlockHeight, syncStateResp.Statehash, err)

// 	syncHandler.remotePeerState = syncStateResp
// 	return err
// }

// func (syncHandler *stateSyncHandler) beforeQueryLedger(e *fsm.Event) {
// 	err := syncHandler.onRecvLedgerInfo(e)
// 	if err == nil {
// 		err = syncHandler.sendLedgerInfo(pb.SyncMsg_SYNC_QUERY_LEDGER_ACK)

// 	}
// 	if err != nil {
// 		e.Cancel(err)
// 	}
// }

// func (syncHandler *stateSyncHandler) beforeQueryLedgerResponse(e *fsm.Event) {
// 	syncHandler.onRecvLedgerInfo(e)
// }

// func (syncHandler *stateSyncHandler) runSyncBlock(ctx context.Context, targetState []byte) error {

// 	syncHandler.client = newSyncer(ctx, syncHandler)

// 	defer logger.Infof("[%s]: Exit. Remote peer <%s>", flogging.GoRDef, syncHandler.remotePeerIdName())
// 	defer syncHandler.fini()

// 	logger.Infof("[%s]: Enter. Remote peer <%s>", flogging.GoRDef, syncHandler.remotePeerIdName())
// 	//---------------------------------------------------------------------------
// 	// 1. query
// 	//---------------------------------------------------------------------------
// 	mostRecentIdenticalHistoryPosition, endBlockNumber, err := syncHandler.client.getSyncTargetBlockNumber()

// 	if mostRecentIdenticalHistoryPosition >= endBlockNumber {
// 		logger.Infof("[%s]: No sync required. mostRecentIdenticalHistoryPosition: %d, endBlockNumber: %d",
// 			flogging.GoRDef, mostRecentIdenticalHistoryPosition, endBlockNumber)
// 		return nil
// 	}

// 	if err != nil {
// 		logger.Errorf("[%s]: getSyncTargetBlockNumber err: %s", flogging.GoRDef, err)
// 		return err
// 	}

// 	startBlockNumber := mostRecentIdenticalHistoryPosition + 1

// 	logger.Infof("[%s]: Query completed. Most recent identical block: %d, target block number: <%d-%d>",
// 		flogging.GoRDef, mostRecentIdenticalHistoryPosition, startBlockNumber, endBlockNumber)

// 	//---------------------------------------------------------------------------
// 	// 2. switch to the right checkpoint
// 	//---------------------------------------------------------------------------
// 	enableStatesyncTest := viper.GetBool("peer.enableStatesyncTest")
// 	if !enableStatesyncTest {

// 		checkpointPosition, err := syncHandler.client.switchToBestCheckpoint(mostRecentIdenticalHistoryPosition)
// 		if err != nil {
// 			logger.Errorf("[%s]: InitiateSync, switchToBestCheckpoint err: %s", flogging.GoRDef, err)

// 			return err
// 		}
// 		startBlockNumber = checkpointPosition + 1
// 		logger.Infof("[%s]: InitiateSync, switch done, startBlockNumber<%d>, endBlockNumber<%d>",
// 			flogging.GoRDef, startBlockNumber, endBlockNumber)
// 	}
// 	//---------------------------------------------------------------------------
// 	// 3. sync detals & blocks
// 	//---------------------------------------------------------------------------
// 	// go to syncdelta state
// 	syncHandler.fsmHandler.Event(enterGetDelta)
// 	handler := newBlockMessageHandler(startBlockNumber, endBlockNumber, syncHandler.client)
// 	syncHandler.client.syncMessageHandler = handler
// 	err = syncHandler.client.executeSync() // sync block cf

// 	if err != nil {
// 		logger.Errorf("[%s]: Failed to sync state detals. err: %s", flogging.GoRDef, err)
// 		return err
// 	}
// 	logger.Infof("[%s]: Sync state detals completed successfully!", flogging.GoRDef)

// 	return err
// }

// //---------------------------------------------------------------------------
// // 1. acknowledge sync start request
// //---------------------------------------------------------------------------
// func (syncHandler *stateSyncHandler) beforeSyncStart(e *fsm.Event) {

// 	logger.Infof("[%s]: sync beforeSyncStart done", flogging.GoRDef)

// 	startRequest := &pb.SyncStartRequest{}
// 	syncMsg := syncHandler.onRecvSyncMsg(e, startRequest)

// 	if syncMsg == nil {
// 		e.Cancel(fmt.Errorf("unexpected sync message"))
// 		return
// 	}

// 	syncHandler.server = newStateServer(syncHandler)
// 	syncHandler.server.correlationId = syncMsg.CorrelationId
// 	resp := &pb.SyncStartResponse{}

// 	var err error
// 	if startRequest.PayloadType == pb.SyncType_SYNC_BLOCK {
// 		resp.BlockHeight, err = syncHandler.server.ledger.GetBlockchainSize()
// 	} else if startRequest.PayloadType == pb.SyncType_SYNC_STATE {
// 		err = syncHandler.server.initStateSync(startRequest, resp)
// 	}

// 	if err != nil {
// 		resp.RejectedReason = fmt.Sprintf("%s", err)
// 		logger.Errorf("[%s]: RejectedReason: %s", flogging.GoRDef, resp.RejectedReason)
// 		e.Cancel(err)
// 	}

// 	err = syncHandler.this.sendSyncMsg(e, pb.SyncMsg_SYNC_SESSION_START_ACK, resp)
// 	if err != nil {
// 		syncHandler.server.ledger.Release()
// 	}
// }

// func (syncHandler *stateSyncHandler) fini() {

// 	err := syncHandler.sendSyncMsg(nil, pb.SyncMsg_SYNC_SESSION_END, nil)
// 	if err != nil {
// 		logger.Errorf("[%s]: sendSyncMsg SyncMsg_SYNC_SESSION_END err: %s", flogging.GoRDef, err)
// 	}

// 	syncHandler.fsmHandler.Event(enterSyncFinish)
// }

// func (syncHandler *stateSyncHandler) sendSyncMsg(e *fsm.Event, msgType pb.SyncMsg_Type, payloadMsg proto.Message) error {

// 	logger.Debugf("%s: <%s> to <%s>", flogging.GoRDef, msgType.String(), syncHandler.remotePeerIdName())
// 	var data = []byte(nil)

// 	if payloadMsg != nil {
// 		tmp, err := proto.Marshal(payloadMsg)

// 		if err != nil {
// 			lerr := fmt.Errorf("Error Marshalling payload message for <%s>: %s", msgType.String(), err)
// 			logger.Info(lerr.Error())
// 			if e != nil {
// 				e.Cancel(&fsm.NoTransitionError{Err: lerr})
// 			}
// 			return lerr
// 		}
// 		data = tmp
// 	}

// 	stream := syncHandler.streamStub.PickHandler(syncHandler.remotePeerId)

// 	if stream == nil {
// 		return fmt.Errorf("Failed to pick handler: %s", syncHandler.remotePeerId)
// 	}

// 	err := stream.SendMessage(&pb.SyncMsg{
// 		Type:    msgType,
// 		Payload: data})

// 	if err != nil {
// 		logger.Errorf("Error sending %s : %s", msgType, err)
// 	}

// 	return err
// }

// func (syncHandler *stateSyncHandler) onRecvSyncMsg(e *fsm.Event, payloadMsg proto.Message) *pb.SyncMsg {

// 	logger.Debugf("%s: from <%s>", flogging.GoRDef, syncHandler.remotePeerIdName())

// 	if _, ok := e.Args[0].(*pb.SyncMsg); !ok {
// 		e.Cancel(fmt.Errorf("Received unexpected sync message type"))
// 		return nil
// 	}
// 	msg := e.Args[0].(*pb.SyncMsg)

// 	if payloadMsg != nil {
// 		err := proto.Unmarshal(msg.Payload, payloadMsg)
// 		if err != nil {
// 			e.Cancel(fmt.Errorf("Error unmarshalling %s: %s", msg.Type.String(), err))
// 			return nil
// 		}
// 	}

// 	logger.Debugf("<%s> from <%s>", msg.Type.String(), syncHandler.remotePeerIdName())
// 	return msg
// }

// func (h *stateSyncHandler) leaveIdle(e *fsm.Event) {

// 	stateUpdate := "leaveIdle"
// 	h.dumpStateUpdate(stateUpdate)
// }

// func (h *stateSyncHandler) enterIdle(e *fsm.Event) {

// 	stateUpdate := "enterIdle"
// 	h.dumpStateUpdate(stateUpdate)

// 	if h.client != nil {
// 		h.client.fini()
// 	}

// }

// func (h *stateSyncHandler) dumpStateUpdate(stateUpdate string) {
// 	logger.Debugf("%s: StateSyncHandler Syncing state update: %s. correlationId<%d>, remotePeerId<%s>", flogging.GoRDef,
// 		stateUpdate, 0, h.remotePeerIdName())
// }

// func (h *stateSyncHandler) remotePeerIdName() string {
// 	return h.remotePeerId.GetName()
// }

// func (h *stateSyncHandler) Tag() string { return "StateSyncStub" }

// func (h *stateSyncHandler) EnableLoss() bool { return false }

// func (h *stateSyncHandler) NewMessage() proto.Message { return new(pb.SyncMsg) }

// func (h *stateSyncHandler) HandleMessage(s *pb.StreamHandler, m proto.Message) error {

// 	wrapmsg := m.(*pb.SyncMsg)

// 	err := h.fsmHandler.Event(wrapmsg.Type.String(), wrapmsg)

// 	//CAUTION: DO NOT return error in non-fatal case or you will end the stream
// 	if err != nil {

// 		if _, ok := err.(ErrHandlerFatal); ok {
// 			return err
// 		}

// 		msg := fmt.Sprintf("%s", err)
// 		if "no transition" != msg {
// 			logger.Errorf("Handle sync message <%s> fail: %s", wrapmsg.Type.String(), err)
// 		}
// 	}

// 	return nil
// }

// func (h *stateSyncHandler) BeforeSendMessage(proto.Message) error {

// 	return nil
// }

// func (h *stateSyncHandler) OnWriteError(e error) {
// 	logger.Error("Sync handler encounter writer error:", e)
// }

// func (h *stateSyncHandler) getClient() *syncer {
// 	return h.client
// }
// func (h *stateSyncHandler) getServer() *stateServer {
// 	return h.server
// }

// func (syncHandler *stateSyncHandler) runSyncState(ctx context.Context, targetStateHash []byte) error {

// 	var err error
// 	var hash []byte

// 	syncHandler.client = newSyncer(ctx, syncHandler)

// 	defer logger.Infof("[%s]: Exit. remotePeerIdName <%s>", flogging.GoRDef, syncHandler.remotePeerIdName())
// 	defer syncHandler.fini()

// 	//---------------------------------------------------------------------------
// 	// 1. query local break point state hash and state offset
// 	//---------------------------------------------------------------------------

// 	syncHandler.client.ledger.EmptyState()
// 	syncHandler.client.syncMessageHandler = newStateMessageHandler(syncHandler.client)
// 	//---------------------------------------------------------------------------
// 	// 2. handshake: send break point state hash and state offset to peer
// 	//---------------------------------------------------------------------------
// 	req := syncHandler.client.syncMessageHandler.produceSyncStartRequest()
// 	_, err = syncHandler.client.issueSyncRequest(req)
// 	if err == nil {
// 		// sync all k-v(s)
// 		err = syncHandler.client.executeSync() // sync state cf
// 	}

// 	//---------------------------------------------------------------------------
// 	// 3. clear persisted position
// 	//---------------------------------------------------------------------------
// 	if err == nil {
// 		//syncHandler.client.ledger.ClearStateOffsetFromDB()
// 		hash, _ = syncHandler.client.ledger.GetCurrentStateHash()
// 		logger.Debugf("GetCurrentStateHash: <%x>", hash)
// 	}
// 	return err
// }
