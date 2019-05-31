package sync

import (
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/ledger/testutil"
	pb "github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"testing"
)

type testFactory struct {
	ctx context.Context
	l   *ledger.Ledger
	opt *syncOpt
}

func (f *testFactory) NewClientStream(*grpc.ClientConn) (grpc.ClientStream, error) {
	//f.tb.Fatal("can not called this")
	return nil, nil
}
func (f *testFactory) NewStreamHandlerImpl(id *pb.PeerID, _ *pb.StreamStub, _ bool) (pb.StreamHandlerImpl, error) {
	return newSyncHandler(f.ctx, id, f.l, f.opt), nil
}

func (f *testFactory) preparePeer(tag string) *pb.SimuPeerStub {
	return pb.NewSimuPeerStub2(pb.NewStreamStub(f, &pb.PeerID{Name: tag}))
}

func GenTestSyncHub(ctx context.Context, l *ledger.Ledger, opt *syncOpt) *pb.SimuPeerStub {

	tf := &testFactory{ctx, l, opt}

	return tf.preparePeer(l.Tag())
}

func PushLedgerStatusOfStub(tb testing.TB, ctx context.Context, simustub *pb.SimuPeerStub, st *pb.LedgerState) {
	for strm := range simustub.OverAllHandlers(ctx) {
		castedh := strm.Impl().(*syncHandler)
		err := castedh.PushLocalLedgerState(strm.StreamHandler, st)
		testutil.AssertNoError(tb, err, "state pushing")
	}
}

var SyncPacketCommHelper = packageMsgHelper

func packageMsgHelper() proto.Message {
	return new(pb.SyncMsg)
}
