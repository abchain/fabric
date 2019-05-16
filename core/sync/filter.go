package sync

import (
	"fmt"
	pb "github.com/abchain/fabric/protos"
)

type StreamFilter struct {
	*pb.PeerEndpoint
}

func (self StreamFilter) QualitifiedPeer(ep *pb.PeerEndpoint) bool {

	//infact we should not need to check if endpoint is myself because it was impossible
	if self.PeerEndpoint != nil && self.ID.Name == ep.ID.Name {
		return false
	}

	//currently state transfer only work for validator
	return ep.Type == pb.PeerEndpoint_VALIDATOR
}

type NewPeerHandshake struct{}

func updateRemoteLedger(strm *pb.StreamHandler, h *syncHandler) error {

	retC, err := h.core.Request(strm,
		&pb.SimpleReq{
			Req: &pb.SimpleReq_State{
				State: h.localLedger.GetLedgerStatus(),
			},
		})
	if err != nil {
		return err
	}

	ret, ok := <-retC
	if !ok {
		return fmt.Errorf("channel fail")
	} else if serr := ret.GetErr(); serr != nil {
		return fmt.Errorf("response fail: %s", serr.GetErrorDetail())
	} else if s := ret.GetSimple().GetState(); s == nil {
		return fmt.Errorf("wrong payload on resp %v", ret)
	} else {
		h.rDataLock.Lock()
		defer h.rDataLock.Unlock()
		h.remoteLedgerState = s
		logger.Infof("Get remote ledeger of peer <%v>: %v", h.remotePeerId, s)
	}
	return nil
}

func (NewPeerHandshake) NotifyNewPeer(peer *pb.PeerID, stub *pb.StreamStub) {

	strm := stub.PickHandler(peer)
	if strm != nil {
		castedh := strm.Impl().(*syncHandler)
		logger.Debugf("Start push/pull local ledeger status to <%v>", peer)

		if err := updateRemoteLedger(strm, castedh); err != nil {
			logger.Errorf("Push/Pull local ledeger status to <%v> failed: %s", peer, err)
		}
	}
}
