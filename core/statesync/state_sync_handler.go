package statesync

import (
	"fmt"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/flogging"
	pb "github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
	"github.com/looplab/fsm"
	"github.com/spf13/viper"
	_ "github.com/spf13/viper"
	"golang.org/x/net/context"
)

func init() {
}


type ISyncHandler interface {
	getFsm() *fsm.FSM
	beforeQueryLedger(e *fsm.Event)
	beforeQueryLedgerResponse(e *fsm.Event)
	beforeSyncStart(e *fsm.Event)
	sendSyncMsg(e *fsm.Event, msgType pb.SyncMsg_Type, payloadMsg proto.Message) error
	onRecvSyncMsg(e *fsm.Event, payloadMsg proto.Message) *pb.SyncMsg
	leaveIdle(e *fsm.Event)
	enterIdle(e *fsm.Event)
	remotePeerIdName() string
	HandleMessage(m proto.Message) error
	getServer() *stateServer
	getClient() *syncer
}

type stateSyncHandler struct {
	this ISyncHandler
	remotePeerId *pb.PeerID
	fsmHandler   *fsm.FSM
	server       *stateServer
	client       *syncer
	streamStub   *pb.StreamStub
	ledger       *ledger.Ledger
	remotePeerState *pb.SyncStateResp
}

type ErrHandlerFatal struct {
	error
}

func newStateSyncHandler(remoterId *pb.PeerID, l *ledger.Ledger, sstub *pb.StreamStub) *stateSyncHandler {
	logger.Debug("create handler for peer", remoterId)

	h := &stateSyncHandler{
		remotePeerId: remoterId,
		streamStub:   sstub,
		ledger:       l,
	}
	h.this = h
	h.fsmHandler = newFsmHandler(h)
	return h
}


func (syncHandler *stateSyncHandler) handshake() error {
	return syncHandler.sendLedgerInfo(pb.SyncMsg_SYNC_QUERY_LEDGER)
}

func (syncHandler *stateSyncHandler) getFsm() *fsm.FSM {
	return syncHandler.fsmHandler
}

func (syncHandler *stateSyncHandler) sendLedgerInfo(code pb.SyncMsg_Type) error {

	blockHeight := syncHandler.ledger.GetBlockchainSize()

	localStatehash, err := syncHandler.ledger.GetCurrentStateHash()
	if err == nil {
		err = syncHandler.sendSyncMsg(nil, code, &pb.SyncStateResp{
			Statehash:   localStatehash,
			BlockHeight: blockHeight,})
	}
	return err
}

func (syncHandler *stateSyncHandler) onRecvLedgerInfo(e *fsm.Event) error {

	syncStateResp := &pb.SyncStateResp{}
	syncMsg := syncHandler.onRecvSyncMsg(e, syncStateResp)

	var err error
	if syncMsg == nil {
		err =fmt.Errorf("unexpected sync message")
		e.Cancel(err)
	}

	logger.Infof("Remote peer[%s]: block height<%d>, statehash<%x>. <%s>", syncHandler.remotePeerId,
		syncStateResp.BlockHeight, syncStateResp.Statehash, err)

	syncHandler.remotePeerState = syncStateResp
	return err
}


func (syncHandler *stateSyncHandler) beforeQueryLedger(e *fsm.Event) {
	err := syncHandler.onRecvLedgerInfo(e)
	if err == nil {
		err = syncHandler.sendLedgerInfo(pb.SyncMsg_SYNC_QUERY_LEDGER_ACK)

	}
	if err != nil {
		e.Cancel(err)
	}
}

func (syncHandler *stateSyncHandler) beforeQueryLedgerResponse(e *fsm.Event) {
	syncHandler.onRecvLedgerInfo(e)
}

func (syncHandler *stateSyncHandler) runSyncBlock(ctx context.Context, targetState []byte) error {

	syncHandler.client = newSyncer(ctx, syncHandler)

	defer logger.Infof("[%s]: Exit. Remote peer <%s>", flogging.GoRDef, syncHandler.remotePeerIdName())
	defer syncHandler.fini()

	logger.Infof("[%s]: Enter. Remote peer <%s>", flogging.GoRDef, syncHandler.remotePeerIdName())
	//---------------------------------------------------------------------------
	// 1. query
	//---------------------------------------------------------------------------
	mostRecentIdenticalHistoryPosition, endBlockNumber, err := syncHandler.client.getSyncTargetBlockNumber()

	if mostRecentIdenticalHistoryPosition >= endBlockNumber {
		logger.Infof("[%s]: No sync required. mostRecentIdenticalHistoryPosition: %d, endBlockNumber: %d",
			flogging.GoRDef, mostRecentIdenticalHistoryPosition, endBlockNumber)
		return nil
	}

	if err != nil {
		logger.Errorf("[%s]: getSyncTargetBlockNumber err: %s", flogging.GoRDef, err)
		return err
	}

	startBlockNumber := mostRecentIdenticalHistoryPosition + 1

	logger.Infof("[%s]: Query completed. Most recent identical block: %d, target block number: <%d-%d>",
		flogging.GoRDef, mostRecentIdenticalHistoryPosition, startBlockNumber, endBlockNumber)

	//---------------------------------------------------------------------------
	// 2. switch to the right checkpoint
	//---------------------------------------------------------------------------
	enableStatesyncTest := viper.GetBool("peer.enableStatesyncTest")
	if !enableStatesyncTest {

		checkpointPosition, err := syncHandler.client.switchToBestCheckpoint(mostRecentIdenticalHistoryPosition)
		if err != nil {
			logger.Errorf("[%s]: InitiateSync, switchToBestCheckpoint err: %s", flogging.GoRDef, err)

			return err
		}
		startBlockNumber = checkpointPosition + 1
		logger.Infof("[%s]: InitiateSync, switch done, startBlockNumber<%d>, endBlockNumber<%d>",
			flogging.GoRDef, startBlockNumber, endBlockNumber)
	}
	//---------------------------------------------------------------------------
	// 3. sync detals & blocks
	//---------------------------------------------------------------------------
	// go to syncdelta state
	syncHandler.fsmHandler.Event(enterGetDelta)
	handler := newBlockMessageHandler(startBlockNumber, endBlockNumber, syncHandler.client)
	syncHandler.client.syncMessageHandler = handler
	err = syncHandler.client.executeSync() // sync block cf

	if err != nil {
		logger.Errorf("[%s]: Failed to sync state detals. err: %s", flogging.GoRDef, err)
		return err
	}
	logger.Infof("[%s]: Sync state detals completed successfully!", flogging.GoRDef)

	return err
}

//---------------------------------------------------------------------------
// 1. acknowledge sync start request
//---------------------------------------------------------------------------
func (syncHandler *stateSyncHandler) beforeSyncStart(e *fsm.Event) {

	logger.Infof("[%s]: sync beforeSyncStart done", flogging.GoRDef)

	startRequest := &pb.SyncStartRequest{}
	syncMsg := syncHandler.onRecvSyncMsg(e, startRequest)

	if syncMsg == nil {
		e.Cancel(fmt.Errorf("unexpected sync message"))
		return
	}

	syncHandler.server = newStateServer(syncHandler)
	syncHandler.server.correlationId = syncMsg.CorrelationId
	resp := &pb.SyncStartResponse{}

	var err error
	if startRequest.PayloadType == pb.SyncType_SYNC_BLOCK {
		resp.BlockHeight, err = syncHandler.server.ledger.GetBlockchainSize()
	} else if startRequest.PayloadType == pb.SyncType_SYNC_STATE {
		err = syncHandler.server.initStateSync(startRequest, resp)
	}

	if err != nil {
		resp.RejectedReason = fmt.Sprintf("%s", err)
		logger.Errorf("[%s]: RejectedReason: %s", flogging.GoRDef, resp.RejectedReason)
		e.Cancel(err)
	}

	err = syncHandler.this.sendSyncMsg(e, pb.SyncMsg_SYNC_SESSION_START_ACK, resp)
	if err != nil {
		syncHandler.server.ledger.Release()
	}
}

func (syncHandler *stateSyncHandler) fini() {

	err := syncHandler.sendSyncMsg(nil, pb.SyncMsg_SYNC_SESSION_END, nil)
	if err != nil {
		logger.Errorf("[%s]: sendSyncMsg SyncMsg_SYNC_SESSION_END err: %s", flogging.GoRDef, err)
	}

	syncHandler.fsmHandler.Event(enterSyncFinish)
}

func (syncHandler *stateSyncHandler) sendSyncMsg(e *fsm.Event, msgType pb.SyncMsg_Type, payloadMsg proto.Message) error {

	logger.Debugf("%s: <%s> to <%s>", flogging.GoRDef, msgType.String(), syncHandler.remotePeerIdName())
	var data = []byte(nil)

	if payloadMsg != nil {
		tmp, err := proto.Marshal(payloadMsg)

		if err != nil {
			lerr := fmt.Errorf("Error Marshalling payload message for <%s>: %s", msgType.String(), err)
			logger.Info(lerr.Error())
			if e != nil {
				e.Cancel(&fsm.NoTransitionError{Err: lerr})
			}
			return lerr
		}
		data = tmp
	}

	stream := syncHandler.streamStub.PickHandler(syncHandler.remotePeerId)

	if stream == nil {
		return fmt.Errorf("Failed to pick handler: %s", syncHandler.remotePeerId)
	}

	err := stream.SendMessage(&pb.SyncMsg{
		Type:    msgType,
		Payload: data})

	if err != nil {
		logger.Errorf("Error sending %s : %s", msgType, err)
	}

	return err
}

func (syncHandler *stateSyncHandler) onRecvSyncMsg(e *fsm.Event, payloadMsg proto.Message) *pb.SyncMsg {

	logger.Debugf("%s: from <%s>", flogging.GoRDef, syncHandler.remotePeerIdName())

	if _, ok := e.Args[0].(*pb.SyncMsg); !ok {
		e.Cancel(fmt.Errorf("Received unexpected sync message type"))
		return nil
	}
	msg := e.Args[0].(*pb.SyncMsg)

	if payloadMsg != nil {
		err := proto.Unmarshal(msg.Payload, payloadMsg)
		if err != nil {
			e.Cancel(fmt.Errorf("Error unmarshalling %s: %s", msg.Type.String(), err))
			return nil
		}
	}

	logger.Debugf("<%s> from <%s>", msg.Type.String(), syncHandler.remotePeerIdName())
	return msg
}

func (h *stateSyncHandler) leaveIdle(e *fsm.Event) {

	stateUpdate := "leaveIdle"
	h.dumpStateUpdate(stateUpdate)
}

func (h *stateSyncHandler) enterIdle(e *fsm.Event) {

	stateUpdate := "enterIdle"
	h.dumpStateUpdate(stateUpdate)

	if h.client != nil {
		h.client.fini()
	}

}

func (h *stateSyncHandler) dumpStateUpdate(stateUpdate string) {
	logger.Debugf("%s: StateSyncHandler Syncing state update: %s. correlationId<%d>, remotePeerId<%s>", flogging.GoRDef,
		stateUpdate, 0, h.remotePeerIdName())
}

func (h *stateSyncHandler) remotePeerIdName() string {
	return h.remotePeerId.GetName()
}

func (h *stateSyncHandler) Stop() { return }

func (h *stateSyncHandler) Tag() string { return "StateSyncStub" }

func (h *stateSyncHandler) EnableLoss() bool { return false }

func (h *stateSyncHandler) NewMessage() proto.Message { return new(pb.SyncMsg) }

func (h *stateSyncHandler) HandleMessage(m proto.Message) error {

	wrapmsg := m.(*pb.SyncMsg)

	err := h.fsmHandler.Event(wrapmsg.Type.String(), wrapmsg)

	//CAUTION: DO NOT return error in non-fatal case or you will end the stream
	if err != nil {

		if _, ok := err.(ErrHandlerFatal); ok {
			return err
		}

		msg := fmt.Sprintf("%s", err)
		if "no transition" != msg {
			logger.Errorf("Handle sync message <%s> fail: %s", wrapmsg.Type.String(), err)
		}
	}

	return nil
}

func (h *stateSyncHandler) BeforeSendMessage(proto.Message) error {

	return nil
}

func (h *stateSyncHandler) OnWriteError(e error) {
	logger.Error("Sync handler encounter writer error:", e)
}

func (h *stateSyncHandler) getClient() *syncer{
	return h.client
}
func (h *stateSyncHandler) getServer() *stateServer{
	return h.server
}

func (syncHandler *stateSyncHandler) runSyncState(ctx context.Context, targetStateHash []byte) error {

	var err error
	var hash []byte

	syncHandler.client = newSyncer(ctx, syncHandler)

	defer logger.Infof("[%s]: Exit. remotePeerIdName <%s>", flogging.GoRDef, syncHandler.remotePeerIdName())
	defer syncHandler.fini()

	//---------------------------------------------------------------------------
	// 1. query local break point state hash and state offset
	//---------------------------------------------------------------------------

	syncHandler.client.ledger.EmptyState()
	syncHandler.client.syncMessageHandler = newStateMessageHandler(syncHandler.client)
	//---------------------------------------------------------------------------
	// 2. handshake: send break point state hash and state offset to peer
	//---------------------------------------------------------------------------
	req := syncHandler.client.syncMessageHandler.produceSyncStartRequest()
	_, err = syncHandler.client.issueSyncRequest(req)
	if err == nil {
		// sync all k-v(s)
		err = syncHandler.client.executeSync() // sync state cf
	}

	//---------------------------------------------------------------------------
	// 3. clear persisted position
	//---------------------------------------------------------------------------
	if err == nil {
		//syncHandler.client.ledger.ClearStateOffsetFromDB()
		hash, _ = syncHandler.client.ledger.GetCurrentStateHash()
		logger.Debugf("GetCurrentStateHash: <%x>", hash)
	}
	return err
}
