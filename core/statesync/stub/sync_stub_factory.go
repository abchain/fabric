package stub

import (
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/peer"
	"github.com/abchain/fabric/core/statesync"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"github.com/op/go-logging"
)

var logger = logging.MustGetLogger("SyncFactory")

//type SyncFactory func(*pb.PeerID, string, *pb.StreamStub) pb.StreamHandlerImpl
type SyncFactory struct {
	stateSyncStub *statesync.StateSyncStub
}

func InitStateSyncStub(bindPeer peer.Peer, l *ledger.Ledger, srv *grpc.Server) *statesync.StateSyncStub {

	sstub := statesync.NewStateSyncStubWithPeer(bindPeer, l)
	if sstub == nil {
		logger.Errorf("Failed to NewStateSyncStubWithPeer:")

		return nil
	}

	err := bindPeer.AddStreamStub("sync", SyncFactory{sstub}, sstub)
	if err != nil {
		logger.Errorf("Failed to AddStreamStub: %s", err)
		panic("Failed to AddStreamStub")
		return nil
	}

	sstub.StreamStub = bindPeer.GetStreamStub("sync")
	if sstub.StreamStub == nil {
		//sanity check
		panic("When streamstub is succefully added, it should not vanish here")
	}

	pb.RegisterSyncServer(srv, SyncFactory{sstub})
	return sstub
}

func (t SyncFactory) NewStreamHandlerImpl(id *pb.PeerID, sstub *pb.StreamStub, initiated bool) (pb.StreamHandlerImpl, error) {

	return t.stateSyncStub.CreateSyncHandler(id, sstub), nil
}

func (t SyncFactory) NewClientStream(conn *grpc.ClientConn) (grpc.ClientStream, error) {

	serverClient := pb.NewSyncClient(conn)
	ctx := context.Background()
	stream, err := serverClient.Data(ctx)

	if err != nil {
		return nil, err
	}

	return stream, nil
}

func (t SyncFactory) Data(stream pb.Sync_DataServer) error {
	return t.stateSyncStub.HandleServer(stream)
}

//func (t SyncFactory) Control(stream pb.Sync_ControlServer) error {
//
//	return nil
//}
