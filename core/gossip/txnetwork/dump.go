package txnetwork

import (
	model "github.com/abchain/fabric/core/gossip/model"
	pb "github.com/abchain/fabric/protos"
)

func (e *TxNetworkEntry) DumpStatus() (s *pb.PeerTxState, p int) {

	e.net.RLock()
	defer e.net.RUnlock()

	s, _ = e.net.peers.QuerySelf()
	p = len(e.net.peers.lruIndex)

	return
}

func DumpNetwork() map[string]func() map[string]*pb.PeerTxState {

	ret := make(map[string]func() map[string]*pb.PeerTxState)
	global.Lock()
	defer global.Unlock()

	for stub, net := range global.ind {

		peers := net.peers
		ret[stub.GetSelf().GetName()] = func() map[string]*pb.PeerTxState {

			out := make(map[string]*pb.PeerTxState)

			net.RLock()
			defer net.RUnlock()
			//copy index

			for id, item := range peers.lruIndex {
				if id == peers.selfId {
					id = "@" + id
				}
				out[id] = item.Value.(*peerStatusItem).PeerTxState
			}

			return out
		}
	}

	return ret
}

func DumpTxNetwork() map[string]func() map[string]uint64 {

	ret := make(map[string]func() map[string]uint64)
	global.Lock()
	defer global.Unlock()

	for stub, _ := range global.ind {

		ch := stub.GetCatalogHandler(hotTxCatName)

		ret[stub.GetSelf().GetName()] = func() map[string]uint64 {

			m := ch.Model()
			smodel := model.DumpScuttlebutt(m)
			out := make(map[string]uint64)

			m.Lock()
			defer m.Unlock()
			//copy index

			for id, s := range smodel.Peers {
				if id == smodel.SelfID {
					id = "@" + id
				}
				to_out, ok := s.To().(standardVClock)
				if ok {
					out[id] = uint64(to_out)
				} else {
					out[id] = 0
				}

			}

			return out
		}
	}

	return ret
}
