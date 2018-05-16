package stub

import (
	"fmt"
	pb "github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
	"github.com/op/go-logging"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

var logger = logging.MustGetLogger("gossipstub")

type GossipHandler interface {
	HandleMessage(*pb.Gossip) error
	Stop()
}

type GossipHandlerImpl struct {
	GossipHandler
}

func (h *GossipHandlerImpl) Tag() string { return "Gossip" }

func (h *GossipHandlerImpl) EnableLoss() bool { return true }

func (h *GossipHandlerImpl) NewMessage() proto.Message { return new(pb.Gossip) }

func (h *GossipHandlerImpl) HandleMessage(m proto.Message) error {
	return h.HandleMessage(m.(*pb.Gossip))
}

func (h *GossipHandlerImpl) BeforeSendMessage(proto.Message) error {
	return nil
}
func (h *GossipHandlerImpl) OnWriteError(e error) {
	logger.Error("Gossip handler encounter writer error:", e)
}

type GossipFactory func(*pb.PeerID) GossipHandler

var DefaultFactory GossipFactory

func GetDefaultFactory() pb.StreamHandlerFactory { return DefaultFactory }

func (t GossipFactory) NewStreamHandlerImpl(id *pb.PeerID, initiated bool) (pb.StreamHandlerImpl, error) {
	if t == nil {
		return nil, fmt.Errorf("No default factory")
	}

	return &GossipHandlerImpl{t(id)}, nil
}

func (t GossipFactory) NewClientStream(conn *grpc.ClientConn) (grpc.ClientStream, error) {
	if t == nil {
		return nil, fmt.Errorf("No default factory")
	}

	serverClient := pb.NewPeerClient(conn)
	ctx := context.Background()
	stream, err := serverClient.GossipIn(ctx)

	if err != nil {
		return nil, err
	}

	return stream, nil
}
