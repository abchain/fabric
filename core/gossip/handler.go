package gossip

import (
	pb "github.com/abchain/fabric/protos"
)

type handlerCore struct {
	*catalogHandler
	policy CatalogPeerPolicies
}

type handlerImpl struct {
	peer  *pb.PeerID
	cores map[string]*handlerCore
}

func newHandler(peer *pb.PeerID, handlers map[string]*catalogHandler) *handlerImpl {

	cores := make(map[string]*handlerCore)
	for id, h := range handlers {
		cores[id] = &handlerCore{h, h.AssignPeerPolicy()}
	}

	return &handlerImpl{
		peer:  peer,
		cores: cores,
	}
}

func (g *handlerImpl) Stop() {}

func (g *handlerImpl) HandleMessage(msg *pb.Gossip) error {

	core, ok := g.cores[msg.GetCatalog()]
	if !ok {
		logger.Errorf("Recv gossip message with catelog not recognized: ", msg.GetCatalog())
		return nil
	}

	global := core.catalogHandler
	cpo := core.policy

	if msg.GetIsPull() { //handling pulling request
		global.HandleDigest(g.peer, msg, cpo)

	} else if msg.Payload != nil {
		global.HandleUpdate(g.peer, msg, cpo)
	}

	return nil
}
