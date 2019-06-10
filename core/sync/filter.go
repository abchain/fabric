package sync

import (
	"fmt"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"time"
)

type StreamFilter struct {
	*pb.PeerEndpoint
}

func (self StreamFilter) QualitifiedPeer(ep *pb.PeerEndpoint) bool {

	//infact we should not need to check if endpoint is myself because it was impossible
	if self.PeerEndpoint != nil && self.ID.Name == ep.ID.Name {
		logger.Errorf("[%v] can not chat with peer for sync: %v", self.PeerEndpoint, ep.ID.Name)
		return false
	}

	return true
}

type NewPeerHandshake struct{}

func updateRemoteLedger(ctx context.Context, strm *pb.StreamHandler, h *syncHandler) error {

	retC, err := h.core.Request(strm,
		&pb.SimpleReq{
			Req: &pb.SimpleReq_State{
				State: h.localLedger.GetLedgerStatus(),
			},
		})
	if err != nil {
		return err
	}

	select {
	case ret, ok := <-retC:
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
	case <-ctx.Done():
		h.core.CancelRequest(retC)
		return ctx.Err()
	}

}

func (NewPeerHandshake) NotifyNewPeer(peer *pb.PeerID, stub *pb.StreamStub) {

	strm := stub.PickHandler(peer)
	if strm != nil {
		castedh := strm.Impl().(*syncHandler)
		logger.Debugf("Start push/pull local ledeger status to <%v>", peer)
		//TODO: where should we get the timeout?
		ctx, endf := context.WithTimeout(castedh.handlerCtx, 5*time.Second)
		go func() {
			if err := updateRemoteLedger(ctx, strm, castedh); err != nil {
				logger.Errorf("Push/Pull local ledeger status to <%v> failed: %s", peer, err)
			}
			endf()
		}()

	}
}
