package statesync

import (
	"fmt"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/flogging"
	pb "github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
	"github.com/looplab/fsm"
	_ "github.com/spf13/viper"
	"golang.org/x/net/context"
)


type mockSyncHandler struct {
	remotePeerId *pb.PeerID
	fsmHandler   *fsm.FSM
	server       *stateServer
	client       *syncer
	ledger       *ledger.Ledger
	remotePeerState *pb.SyncStateResp

	syncMsgChan chan *pb.SyncMsg //syncMessageChan
	peer mockPeer
}


func newMockSyncHandler(remoterId *pb.PeerID, l *ledger.Ledger) *mockSyncHandler {

	h := &mockSyncHandler{
		remotePeerId: remoterId,
		ledger:       l,
	}
	h.fsmHandler = newFsmHandler(h)
	h.syncMsgChan = make(chan *pb.SyncMsg)
	return h
}


func (syncHandler *mockSyncHandler) handshake() error {
	return syncHandler.sendLedgerInfo(pb.SyncMsg_SYNC_QUERY_LEDGER)
}
func (syncHandler *mockSyncHandler) getFsm() *fsm.FSM {	return syncHandler.fsmHandler}
func (syncHandler *mockSyncHandler) sendLedgerInfo(code pb.SyncMsg_Type) error {	return nil}
func (syncHandler *mockSyncHandler) onRecvLedgerInfo(e *fsm.Event) error {	return nil}
func (syncHandler *mockSyncHandler) beforeQueryLedger(e *fsm.Event) {}
func (syncHandler *mockSyncHandler) beforeQueryLedgerResponse(e *fsm.Event) {}
func (syncHandler *mockSyncHandler) runSyncBlock(ctx context.Context, targetState []byte) error {	return nil}

//---------------------------------------------------------------------------
// 1. acknowledge sync start request
//---------------------------------------------------------------------------
func (syncHandler *mockSyncHandler) beforeSyncStart(e *fsm.Event) {

	logger.Infof("[%s]: sync beforeSyncStart done", flogging.GoRDef)
	startRequest := &pb.SyncStartRequest{}
	syncMsg := syncHandler.onRecvSyncMsg(e, startRequest)
	if syncMsg == nil {
		e.Cancel(fmt.Errorf("unexpected sync message"))
		return
	}

	//syncHandler.server = newStateServer(syncHandler)
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

	err = syncHandler.sendSyncMsg(e, pb.SyncMsg_SYNC_SESSION_START_ACK, resp)
	if err != nil {
		syncHandler.server.ledger.Release()
	}
}

func (syncHandler *mockSyncHandler) fini() {

}


func (h *mockSyncHandler) leaveIdle(e *fsm.Event) {

	stateUpdate := "leaveIdle"
	h.dumpStateUpdate(stateUpdate)
}

func (h *mockSyncHandler) enterIdle(e *fsm.Event) {

	stateUpdate := "enterIdle"
	h.dumpStateUpdate(stateUpdate)

	if h.client != nil {
		h.client.fini()
	}

}


func (h *mockSyncHandler) remotePeerIdName() string {
	return h.remotePeerId.GetName()
}

func (h *mockSyncHandler) Stop() { return }
func (h *mockSyncHandler) Tag() string { return "StateSyncStub" }
func (h *mockSyncHandler) EnableLoss() bool { return false }
func (h *mockSyncHandler) NewMessage() proto.Message { return new(pb.SyncMsg) }
func (h *mockSyncHandler) BeforeSendMessage(proto.Message) error {return nil}
func (h *mockSyncHandler) OnWriteError(e error) {}
func (h *mockSyncHandler) runSyncState(ctx context.Context, targetStateHash []byte) error {return nil}

func (h *mockSyncHandler) getClient() *syncer{
	return h.client
}
func (h *mockSyncHandler) getServer() *stateServer{
	return h.server
}


func (syncHandler *mockSyncHandler) sendSyncMsg(e *fsm.Event, msgType pb.SyncMsg_Type, payloadMsg proto.Message) error {

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

	syncHandler.peer.enqueue(&pb.SyncMsg{
		Type:    msgType,
		Payload: data})

	return nil
}

func (syncHandler *mockSyncHandler) onRecvSyncMsg(e *fsm.Event, payloadMsg proto.Message) *pb.SyncMsg {

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

func (h *mockSyncHandler) dumpStateUpdate(stateUpdate string) {
	logger.Infof("%s: mockSyncHandler Syncing state update: %s. correlationId<%d>, remotePeerId<%s>", flogging.GoRDef,
		stateUpdate, 0, h.remotePeerIdName())
}


func (h *mockSyncHandler) HandleMessage(m proto.Message) error {

	wrapmsg := m.(*pb.SyncMsg)
	err := h.fsmHandler.Event(wrapmsg.Type.String(), wrapmsg)

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

type mockPeer interface {
 	enqueue(msg *pb.SyncMsg)
}

func (handler *mockSyncHandler) enqueue(msg *pb.SyncMsg) {
	handler.syncMsgChan <- msg
}

func (handler *mockSyncHandler) dequeue() (*pb.SyncMsg, error) {
	var err error
	select {
	case syncMessageResp, ok := <- handler.syncMsgChan:
		if !ok {
			err = fmt.Errorf("sync Message channel close")
			break
		}
		return syncMessageResp, nil
	}
	return nil, err
}

func (handler *mockSyncHandler) handleClientChat() error {

	fmt.Printf(" handleClientChat() \n")
	for {
		in, err := handler.dequeue()
		err = handler.HandleMessage(in)
		if err != nil {
			return err
		}
	}
}


func (handler *mockSyncHandler) handleServerChat() error {

	fmt.Printf(" handleServerChat() \n")
	for {
		in, err := handler.dequeue()
		err = handler.HandleMessage(in)
		if err != nil {
			return err
		}
	}
}