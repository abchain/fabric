package stub

import (
	"fmt"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/peer"
	"github.com/abchain/fabric/core/sync"
	pb "github.com/abchain/fabric/protos"
	"google.golang.org/grpc"
)

type syncStubFactory struct {
	*sync.SyncStub
}

type rpcServer struct {
	*pb.StreamStub
}

func (r rpcServer) Data(stream pb.Sync_DataServer) error {
	return r.HandleServer(stream)
}

func init() {
	sync.AccessStubHelper = func(sstub *pb.StreamStub, hf func(*sync.SyncStub)) {
		stubF := sstub.StreamHandlerFactory.(syncStubFactory)
		hf(stubF.SyncStub)
	}
}

func InitSyncStub(bindPeer peer.Peer, l *ledger.Ledger, srv *grpc.Server) *pb.StreamStub {

	ep, _ := bindPeer.GetPeerEndpoint()
	syncstub := sync.NewSyncStub(bindPeer.GetPeerCtx(), l)

	err := bindPeer.AddStreamStub("sync", syncStubFactory{syncstub},
		sync.StreamFilter{ep}, sync.NewPeerHandshake{})
	if err != nil {
		panic(fmt.Errorf("Failed to AddStreamStub: %s", err))
	}

	sstub := bindPeer.GetStreamStub("sync")
	if sstub == nil {
		//sanity check
		panic("When streamstub is succefully added, it should not vanish here")
	}

	l.SubScribeNewBlock(func(blkn uint64, _ *pb.Block) {

		//only push status for each 3 blocks
		if blkn%3 == 0 {
			go syncstub.BroadcastLedgerStatus(sstub)
		}
	})

	pb.RegisterSyncServer(srv, rpcServer{sstub})
	return sstub
}

func (t syncStubFactory) NewClientStream(conn *grpc.ClientConn) (grpc.ClientStream, error) {

	serverClient := pb.NewSyncClient(conn)
	stream, err := serverClient.Data(t.StubContext())

	if err != nil {
		return nil, err
	}

	return stream, nil
}
