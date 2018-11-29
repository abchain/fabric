package statesync

import (
	"fmt"
	_ "github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/peer"
	"github.com/abchain/fabric/core/statesync/stub"
	"github.com/abchain/fabric/flogging"
	pb "github.com/abchain/fabric/protos"
	"github.com/op/go-logging"
	_ "github.com/spf13/viper"
	"golang.org/x/net/context"
	"sync"
)

var logger = logging.MustGetLogger("statesyncstub")


type StateSyncStub struct {
	self *pb.PeerID
	*pb.StreamStub
	sync.RWMutex
	curCorrrelation uint64
	curTask context.Context
}

func NewStateSyncStubWithPeer(p peer.Peer) *StateSyncStub {

	self, err := p.GetPeerEndpoint()
	if err != nil {
		panic("No self endpoint")
	}

	gctx, _ := context.WithCancel(p.GetPeerCtx())
	sycnStub := &StateSyncStub{
		self:    self.ID,
		curTask: gctx,
	}

	err = p.AddStreamStub("sync", stub.DefaultSyncFactory, sycnStub)
	if err != nil {
		logger.Error("Bind sync stub to peer fail: ", err)
		return nil
	}

	syncStreamStub := p.GetStreamStub("sync")
	if syncStreamStub == nil {
		logger.Error("peer have no sync streamstub")
		return nil
	}

	sycnStub.StreamStub = syncStreamStub
	return sycnStub
}

type ErrInProcess struct {
	error
}

type ErrHandlerFatal struct {
	error
}

//if busy, return current correlation Id, els return 0
func (s *StateSyncStub) IsBusy() uint64 {

	s.RLock()
	defer s.RUnlock()

	if s.curTask == nil {
		return uint64(0)
	} else {
		return s.curCorrrelation
	}
}


func (s *StateSyncStub) SyncToState(blockNumber uint64, blockHash []byte, peerIDs []*pb.PeerID) (err error, result bool) {

	result = false

	for _, peer := range peerIDs {
		err = s.SyncToStateByPeer(context.TODO(), blockHash, nil, peer)
		if err == nil {
			result = true
			break
		}
		logger.Errorf("[%s]: %s", flogging.GoRDef, err)
	}

	return err, result
}

func (s *StateSyncStub) Start() {

}

func (s *StateSyncStub) Stop() {

}

func (s *StateSyncStub) SyncToStateByPeer(ctx context.Context, targetState []byte, opt *syncOpt, peer *pb.PeerID) error {

	var err error
	s.Lock()
	if s.curTask != nil {
		s.Unlock()
		return &ErrInProcess{fmt.Errorf("Another task is running")}
	}

	s.curTask = ctx
	s.curCorrrelation++
	s.Unlock()

	// use stream stub get stream handler by PeerId
	// down cast stream handler to stateSyncHandler
	// call stateSyncHandler run
	handler := s.StreamStub.PickHandler(peer)

	if handler == nil {

		logger.Errorf("[%s]: Failed to find sync handler for peer <%v>",
			flogging.GoRDef, peer)

		err = fmt.Errorf("[%s]: Failed to find sync handler for peer <%v>",
			flogging.GoRDef, peer)

		return err
	}

	peerSyncHandler, ok := handler.StreamHandlerImpl.(*stateSyncHandler)

	peerSyncHandler.streamHandler = handler

	if !ok {
		return fmt.Errorf("[%s]: Target peer <%v>, " +
			"failed to convert StreamHandlerImpl to stateSyncHandler",
			flogging.GoRDef, peer)
	}

	peerSyncHandler.run(ctx, targetState)

	defer func() {
		s.Lock()
		s.curTask = nil
		s.Unlock()
	}()

	return err
}

