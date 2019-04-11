package stub

import (
	"github.com/abchain/fabric/core/gossip"
	"github.com/abchain/fabric/core/peer"
	pb "github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
	"github.com/op/go-logging"
	"google.golang.org/grpc"
)

var logger = logging.MustGetLogger("gossip")

//each call of NewGossipWithPeer will travel register collections to create the corresponding catalogy handlers
var RegisterCat []func(*gossip.GossipStub)

//create the corresponding streamstub and bind it with peer and service
func InitGossipStub(bindPeer peer.Peer, srv *grpc.Server) *gossip.GossipStub {

	gstub := gossip.NewGossipWithPeer(bindPeer)
	if gstub == nil {
		return nil
	}

	//gossipStub itself is also a posthandler
	err := bindPeer.AddStreamStub("gossip", GossipFactory{gstub}, gstub)
	if err != nil {
		logger.Error("Bind gossip stub to peer fail: ", err)
		return nil
	}

	gstub.StreamStub = bindPeer.GetStreamStub("gossip")
	if gstub.StreamStub == nil {
		//sanity check
		panic("When streamstub is succefully added, it should not vanish here")
	}

	//reg all catalogs
	for _, f := range RegisterCat {
		f(gstub)
	}

	if srv != nil {
		pb.RegisterGossipServer(srv, GossipFactory{gstub})
	}

	return gstub
}

func init() {

	gossip.ObtainHandler = func(h *pb.StreamHandler) gossip.GossipHandler {
		hh, ok := h.StreamHandlerImpl.(*GossipHandlerImpl)
		if !ok {
			panic("type error, not GossipHandlerImpl")
		}

		return hh.GossipHandler
	}
}

type GossipHandlerImpl struct {
	gossip.GossipHandler
}

func (h GossipHandlerImpl) Tag() string { return "Gossip" }

func (h GossipHandlerImpl) EnableLoss() bool { return true }

func (h GossipHandlerImpl) NewMessage() proto.Message { return new(pb.GossipMsg) }

func (h GossipHandlerImpl) HandleMessage(m proto.Message) error {
	return h.GossipHandler.HandleMessage(m.(*pb.GossipMsg))
}

func (h GossipHandlerImpl) BeforeSendMessage(proto.Message) error {
	return nil
}
func (h GossipHandlerImpl) OnWriteError(e error) {
	logger.Error("Gossip handler encounter writer error:", e)
}

type GossipFactory struct {
	*gossip.GossipStub
}

func (t GossipFactory) NewStreamHandlerImpl(id *pb.PeerID, sstub *pb.StreamStub, initiated bool) (pb.StreamHandlerImpl, error) {

	return &GossipHandlerImpl{t.CreateGossipHandler(id)}, nil
}

func (t GossipFactory) NewClientStream(conn *grpc.ClientConn) (grpc.ClientStream, error) {
	serverClient := pb.NewGossipClient(conn)
	stream, err := serverClient.In(t.GetStubContext())

	if err != nil {
		return nil, err
	}

	return stream, nil
}

func (t GossipFactory) In(stream pb.Gossip_InServer) error {
	return t.GetSStub().HandleServer(stream)
}
