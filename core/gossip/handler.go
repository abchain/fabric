package gossip

import (
	"errors"
	pb "github.com/abchain/fabric/protos"
)

var ObtainHandler func(*pb.StreamHandler) GossipHandler

type handlerImpl struct {
	ppo   PeerPolicies
	cores map[string]CatalogHandler
}

func newHandler(peer *pb.PeerID, handlers map[string]CatalogHandler) *handlerImpl {

	return &handlerImpl{
		ppo:   NewPeerPolicy(peer.GetName()),
		cores: handlers,
	}
}

func (g *handlerImpl) Stop() {

}

func (g *handlerImpl) GetPeerPolicy() CatalogPeerPolicies {
	return g.ppo
}

func (g *handlerImpl) HandleMessage(strm *pb.StreamHandler, msg *pb.GossipMsg) error {

	global, ok := g.cores[msg.GetCatalog()]
	if !ok {
		logger.Error("Recv gossip message with catelog not recognized: ", msg.GetCatalog())
		return nil
	}

	g.ppo.RecvUpdate(msg.EstimateSize())

	if dig := msg.GetDig(); dig != nil {
		global.HandleDigest(strm, dig, g.ppo)
	} else if ud := msg.GetUd(); ud != nil {
		global.HandleUpdate(msg.GetUd(), g.ppo)
	} else {
		g.ppo.RecordViolation(errors.New("Sent empty gossip message"))
	}

	return g.ppo.IsPolicyViolated()
}
