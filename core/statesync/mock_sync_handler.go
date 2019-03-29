package statesync

import (
	"fmt"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/flogging"
	pb "github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
	"github.com/looplab/fsm"
	_ "github.com/spf13/viper"
)

type mockPeer interface {
	enqueue(msg *pb.SyncMsg)
}

type mockSyncHandler struct {
	*stateSyncHandler

	syncMsgChan chan *pb.SyncMsg
	peer mockPeer
}


func newMockSyncHandler(remoterId *pb.PeerID, l *ledger.Ledger) *mockSyncHandler {


	base := newStateSyncHandler(remoterId, l, nil)
	h := &mockSyncHandler{
		stateSyncHandler: base,
	}
	h.this = h
	h.syncMsgChan = make(chan *pb.SyncMsg)
	return h
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
	return handler.handleChat()
}


func (handler *mockSyncHandler) handleServerChat() error {
	return handler.handleChat()
}

func (handler *mockSyncHandler) handleChat() error {

	for {
		in, err := handler.dequeue()
		err = handler.HandleMessage(in)
		if err != nil {
			return err
		}
	}
}