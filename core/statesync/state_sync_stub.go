package statesync

import (
	"fmt"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/peer"
	"github.com/abchain/fabric/flogging"
	pb "github.com/abchain/fabric/protos"
	"github.com/op/go-logging"
	"github.com/spf13/viper"
	"golang.org/x/net/context"
	"sync"
)

var logger = logging.MustGetLogger("statesyncstub")

type StateSyncStub struct {
	self *pb.PeerID
	*pb.StreamStub
	sync.RWMutex
	curCorrrelation uint64
	curTask         context.Context
	ledger          *ledger.Ledger
}

type ErrInProcess struct {
	error
}

func NewStateSyncStubWithPeer(p peer.Peer, l *ledger.Ledger) *StateSyncStub {

	self, err := p.GetPeerEndpoint()
	if err != nil {
		panic("No self endpoint")
	}

	gctx, _ := context.WithCancel(p.GetPeerCtx())

	sycnStub := &StateSyncStub{
		self:    self.ID,
		curTask: gctx,
		ledger:  l,
	}

	return sycnStub
}

func (s *StateSyncStub) SyncToTarget(blockNumber uint64, blockHash []byte, peerIDs []*pb.PeerID) (err error, result bool) {

	result = false

	for _, peer := range peerIDs {
		err = s.SyncToStateByPeer(blockHash, nil, peer, "block")
		if err == nil {
			result = true
			break
		}
		logger.Errorf("[%s]: %s", flogging.GoRDef, err)
	}

	return err, result
}

func (s *StateSyncStub) Configure(vp *viper.Viper) error {
	logger.Debugf("configure item: %v", vp.AllSettings())
	return nil
}

func (s *StateSyncStub) Start() {
}

func (s *StateSyncStub) Stop() {
}

func (s *StateSyncStub) NotifyNewPeer(peer *pb.PeerID) {

	handler := s.StreamStub.PickHandler(peer)
	if handler != nil {
		peerSyncHandler, ok := handler.Impl().(*stateSyncHandler)
		if ok && peerSyncHandler != nil {
			err := peerSyncHandler.handshake()
			if err != nil {
				logger.Errorf("[%s]: Failed to handshake with peer <%+v>. Error: %s", flogging.GoRDef, peer, err)

			}
		}
	}
}

func (s *StateSyncStub) CreateSyncHandler(id *pb.PeerID, sstub *pb.StreamStub) pb.StreamHandlerImpl {

	return newStateSyncHandler(id, s.ledger, sstub)
}

func (s *StateSyncStub) SyncToStateByPeer(targetState []byte, opt *syncOpt,	peer *pb.PeerID, blockSync string) error {

	var err error
	s.Lock()
	s.curCorrrelation++
	s.Unlock()

	// use stream stub get stream handler by PeerId
	// down cast stream handler to stateSyncHandler
	// call stateSyncHandler run

	logger.Debugf("[%s]: StreamStub<%+v>, <%+v>", flogging.GoRDef, s.StreamStub, s)

	handler := s.StreamStub.PickHandler(peer)

	if handler == nil {
		return fmt.Errorf("[%s]: Failed to find sync handler for peer <%v>",flogging.GoRDef, peer)
	}

	peerSyncHandler, ok := handler.Impl().(*stateSyncHandler)

	if !ok {
		return fmt.Errorf("[%s]: Target peer <%v>, "+
			"failed to convert StreamHandlerImpl to stateSyncHandler",
			flogging.GoRDef, peer)
	}

	if blockSync == "block"{
		err = peerSyncHandler.runSyncBlock(s.curTask, targetState)
	} else if blockSync == "state" {
		err = peerSyncHandler.runSyncState(s.curTask, targetState)
	} else {
		panic("Invalid type!")
	}

	defer func() {
		s.Lock()
		s.curTask = nil
		s.Unlock()
	}()

	return err
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
