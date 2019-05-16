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
	err := bindPeer.AddStreamStub("gossip", gossipFactory{gstub}, gstub)
	if err != nil {
		logger.Error("Bind gossip stub to peer fail: ", err)
		return nil
	}

	sstub := bindPeer.GetStreamStub("gossip")
	if sstub == nil {
		//sanity check
		panic("When streamstub is succefully added, it should not vanish here")
	}

	gstub.StreamStub = sstub
	//reg all catalogs
	for _, f := range RegisterCat {
		f(gstub)
	}

	//reg to grpc
	if srv != nil {
		pb.RegisterGossipServer(srv, rpcSrv{sstub})
	}

	return gstub
}

func init() {

	gossip.ObtainHandler = func(h *pb.StreamHandler) gossip.GossipHandler {
		hh, ok := h.Impl().(*gossipHandlerImpl)
		if !ok {
			panic("type error, not GossipHandlerImpl")
		}

		return hh.GossipHandler
	}
}

type gossipHandlerImpl struct {
	gossip.GossipHandler
}

func (h gossipHandlerImpl) Tag() string { return "Gossip" }

func (h gossipHandlerImpl) EnableLoss() bool { return true }

func (h gossipHandlerImpl) NewMessage() proto.Message { return new(pb.GossipMsg) }

func (h gossipHandlerImpl) HandleMessage(strm *pb.StreamHandler, m proto.Message) error {
	return h.GossipHandler.HandleMessage(strm, m.(*pb.GossipMsg))
}

func (h gossipHandlerImpl) BeforeSendMessage(proto.Message) error {
	return nil
}
func (h gossipHandlerImpl) OnWriteError(e error) {
	logger.Error("Gossip handler encounter writer error:", e)
}

type gossipFactory struct {
	*gossip.GossipStub
}

func (t gossipFactory) NewStreamHandlerImpl(id *pb.PeerID, sstub *pb.StreamStub, initiated bool) (pb.StreamHandlerImpl, error) {

	return &gossipHandlerImpl{t.CreateGossipHandler(id)}, nil
}

func (t gossipFactory) NewClientStream(conn *grpc.ClientConn) (grpc.ClientStream, error) {
	serverClient := pb.NewGossipClient(conn)
	stream, err := serverClient.In(t.GetStubContext())

	if err != nil {
		return nil, err
	}

	return stream, nil
}

type rpcSrv struct {
	*pb.StreamStub
}

func (r rpcSrv) In(stream pb.Gossip_InServer) error {
	return r.HandleServer(stream)
}
