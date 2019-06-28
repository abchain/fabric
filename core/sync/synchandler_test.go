package sync

import (
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/ledger/testutil"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"testing"
	"time"
)

func (f *testFactory) prepareServerPeers(tb testing.TB, cli *pb.SimuPeerStub, cnt int) []*pb.SimuPeerStub {
	var ret []*pb.SimuPeerStub
	for i := 0; i < cnt; i++ {
		ret = append(ret, pb.NewSimuPeerStub2(pb.NewStreamStub(f,
			&pb.PeerID{Name: testutil.GenerateID(tb)})))
		err, runf := cli.ConnectTo(f.ctx, ret[i])
		testutil.AssertNoError(tb, err, "run simu peer")
		go func() {
			for f.ctx.Err() == nil {
				runf()
			}
		}()
	}
	return ret
}

func TestTxSync_Basic(t *testing.T) {

	//just made us to see all resouces has been released
	defer time.Sleep(200 * time.Millisecond)

	baseCtx, endAll := context.WithCancel(context.Background())
	defer endAll()

	testLedger := ledger.InitTestLedger(t)
	baseOpt := DefaultSyncOption()
	baseOpt.txOption.maxSyncTxCount = 1

	testBase := &testFactory{baseCtx, testLedger, baseOpt}

	targetStub := testBase.preparePeer("target")
	testBase.prepareServerPeers(t, targetStub, 5)

	testutil.AssertEquals(t, targetStub.HandlerCount(), 5)

	var txids []string
	var txs []*pb.Transaction
	//populate some txs
	for i := 0; i < 20; i++ {
		txids = append(txids, testutil.GenerateID(t))
		transaction, _ := pb.NewTransaction(pb.ChaincodeID{Path: "testUrl"}, txids[i], "", []string{"param1", txids[i]})
		txs = append(txs, transaction)
	}
	testutil.AssertNoError(t, testLedger.PutTransactions(txs), "put txs")

	opt := DefaultClientOption()
	opt.ConcurrentLimit = 3
	opt.RetryFail = true

	opt.RetryCount = 2
	testCF := &txCliFactory{
		opt:         opt,
		assignedCnt: 2,
		target:      txids[:5],
	}

	err := ExecuteSyncTask(baseCtx, testCF, targetStub.StreamStub)

	testutil.AssertNoError(t, err, "sync tx")
	testutil.AssertEquals(t, len(testCF.txout), 5)

}
