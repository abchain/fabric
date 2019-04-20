package gossip

import (
	"github.com/abchain/fabric/core/peer"
	peerACL "github.com/abchain/fabric/core/peer/acl"
	pb "github.com/abchain/fabric/protos"
	logging "github.com/op/go-logging"
	"golang.org/x/net/context"
	"sync"
	"time"
)

var logger = logging.MustGetLogger("gossip")

var globalSeq uint64
var globalSeqLock sync.Mutex

func getGlobalSeq() uint64 {

	globalSeqLock.Lock()
	defer globalSeqLock.Unlock()

	ref := uint64(time.Now().Unix())
	if ref > globalSeq {
		globalSeq = ref

	} else {
		globalSeq++
	}

	return globalSeq
}

//the simplified stream handler
type GossipHandler interface {
	HandleMessage(*pb.StreamHandler, *pb.GossipMsg) error
	Stop()
}

//GossipStub struct
type GossipStub struct {
	self            *pb.PeerID
	disc            peer.Discoverer
	catalogHandlers map[string]CatalogHandler
	newPeerNotify   []CatalogHandlerEx

	*pb.StreamStub
	peerACL.AccessControl
	globalCtx context.Context
}

func (g *GossipStub) CreateGossipHandler(id *pb.PeerID) GossipHandler {
	logger.Debug("create handler for peer", g.self)
	return newHandler(id, g.catalogHandlers)
}

func (g *GossipStub) GetSelf() *pb.PeerID {
	return g.self
}

func (g *GossipStub) GetSStub() *pb.StreamStub {
	return g.StreamStub
}

func (g *GossipStub) GetStubContext() context.Context {
	return g.globalCtx
}

func (g *GossipStub) GetCatalogHandler(cat string) CatalogHandler {
	return g.catalogHandlers[cat]
}

func (g *GossipStub) AddCatalogHandler(h CatalogHandler) {
	_, ok := g.catalogHandlers[h.Name()]
	g.catalogHandlers[h.Name()] = h

	if ok {
		logger.Errorf("Duplicated add handler for catalog %s", h.Name())
	} else {
		logger.Infof("Add handler for catalog %s", h.Name())
	}
}

func (g *GossipStub) SubScribeNewPeerNotify(h CatalogHandler) {
	ex, ok := h.(CatalogHandlerEx)
	if !ok {
		panic("handler did not imply ex interface, wrong code")
	}

	g.newPeerNotify = append(g.newPeerNotify, ex)
}

func (g *GossipStub) NotifyNewPeer(peerid *pb.PeerID) {
	for _, h := range g.newPeerNotify {
		h.OnConnectNewPeer(peerid)
	}
}

func NewGossipWithPeer(p peer.Peer) *GossipStub {

	cache.Do(cacheConfiguration)

	self, err := p.GetPeerEndpoint()
	if err != nil {
		panic("No self endpoint")
	}

	gctx, _ := context.WithCancel(p.GetPeerCtx())

	gossipStub := &GossipStub{
		self:            self.ID,
		catalogHandlers: make(map[string]CatalogHandler),
		//		StreamStub:      p.GetStreamStub("gossip"),
		globalCtx: gctx,
	}

	nb, err := p.GetNeighbour()

	if err != nil {
		logger.Errorf("No neighbour for this peer (%s), gossip run without access control", err)
	} else {
		gossipStub.disc, _ = nb.GetDiscoverer()
		gossipStub.AccessControl, _ = nb.GetACL()
	}

	logger.Infof("A Gossip module created on peer %s", self.ID)
	return gossipStub

}
