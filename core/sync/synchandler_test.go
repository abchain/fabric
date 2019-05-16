package sync

import (
	"fmt"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/ledger/testutil"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"sync"
	"testing"
	"time"
)

type testFactory struct {
	tb  testing.TB
	ctx context.Context
	l   *ledger.Ledger
	opt *syncOpt
}

func (f *testFactory) NewClientStream(*grpc.ClientConn) (grpc.ClientStream, error) {
	f.tb.Fatal("can not called this")
	return nil, nil
}
func (f *testFactory) NewStreamHandlerImpl(id *pb.PeerID, _ *pb.StreamStub, _ bool) (pb.StreamHandlerImpl, error) {
	return newSyncHandler(f.ctx, id, f.l, f.opt), nil
}

func (f *testFactory) preparePeer(tag string) *pb.SimuPeerStub {
	return pb.NewSimuPeerStub2(pb.NewStreamStub(f, &pb.PeerID{Name: tag}))
}

func (f *testFactory) prepareServerPeers(cli *pb.SimuPeerStub, cnt int) []*pb.SimuPeerStub {
	var ret []*pb.SimuPeerStub
	for i := 0; i < cnt; i++ {
		ret = append(ret, pb.NewSimuPeerStub2(pb.NewStreamStub(f,
			&pb.PeerID{Name: testutil.GenerateID(f.tb)})))
		err, runf := cli.ConnectTo(f.ctx, ret[i])
		testutil.AssertNoError(f.tb, err, "run simu peer")
		go func() {
			for f.ctx.Err() == nil {
				runf()
			}
		}()
	}
	return ret
}

type testTxCliFactory struct {
	opt         *clientOpts
	assignedCnt int
	sync.Mutex
	target []string
	txout  []*pb.Transaction
}

func (cf *testTxCliFactory) Tag() string { return "testTxClis" }
func (cf *testTxCliFactory) Opts() *clientOpts {
	return cf.opt
}
func (cf *testTxCliFactory) PreFilter(_ *pb.LedgerState) bool {
	return true
}
func (cf *testTxCliFactory) AssignHandling() func(*pb.StreamHandler, *syncCore) error {

	cf.Lock()
	defer cf.Unlock()
	if len(cf.target) == 0 {
		//done, just assign a empty function
		return func(*pb.StreamHandler, *syncCore) error {
			return NormalEnd{}
		}
	}

	var assignedTask []string
	if cf.assignedCnt >= len(cf.target) {
		assignedTask = cf.target
		cf.target = nil
	} else {
		assignedPos := len(cf.target) - cf.assignedCnt
		cf.target, assignedTask = cf.target[:assignedPos], cf.target[assignedPos:]
	}

	return func(h *pb.StreamHandler, c *syncCore) (err error) {

		var ret []*pb.Transaction
		reside := make(map[string]bool)
		for _, id := range assignedTask {
			reside[id] = false
		}
		defer func() {
			var residearr []string
			for k, done := range reside {
				if !done {
					residearr = append(residearr, k)
				}
			}

			if len(residearr) > 0 {
				clilogger.Debugf("tx sync not finished, resident task: %v", residearr)
				err = fmt.Errorf("Not finished")
			}

			cf.Lock()
			defer cf.Unlock()
			cf.target = append(cf.target, residearr...)
			cf.txout = append(cf.txout, ret...)
		}()

		chn, err := c.Request(h, &pb.SimpleReq{
			Req: &pb.SimpleReq_Tx{Tx: &pb.TxQuery{Txid: assignedTask}},
		})
		if err != nil {
			return err
		}

		select {
		case resp := <-chn:
			if serr := resp.GetErr(); serr != nil {
				return fmt.Errorf("resp err %s", serr.GetErrorDetail())
			} else if txblk := resp.GetSimple().GetTx(); txblk == nil {
				return fmt.Errorf("Empty payload")
			} else {
				for _, tx := range txblk.GetTransactions() {
					if tx != nil {
						reside[tx.GetTxid()] = true
						ret = append(ret, tx)
					}
				}
				return NormalEnd{}
			}

		case <-cf.opt.Done():
			return cf.opt.Err()
		}
	}
}

func TestTxSync_Basic(t *testing.T) {

	//just made us to see all resouces has been released
	defer time.Sleep(200 * time.Millisecond)

	baseCtx, endAll := context.WithCancel(context.Background())
	defer endAll()

	testLedger := ledger.InitTestLedger(t)
	baseOpt := DefaultSyncOption()
	baseOpt.txOption.maxSyncTxCount = 1

	testBase := &testFactory{t, baseCtx, testLedger, baseOpt}

	targetStub := testBase.preparePeer("target")
	testBase.prepareServerPeers(targetStub, 5)

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

	opt := DefaultClientOption(baseCtx)
	opt.ConcurrentLimit = 3
	//we need one retry: one worker may take out 2 resident task once but just
	//obtain 1, left another for more retry...
	opt.RetryCount = 1
	testCF := &testTxCliFactory{
		opt:         opt,
		assignedCnt: 2,
		target:      txids[:5],
	}

	err := ExecuteSyncTask(testCF, targetStub.StreamStub)

	testutil.AssertNoError(t, err, "sync tx")
	testutil.AssertEquals(t, len(testCF.txout), 5)

}
