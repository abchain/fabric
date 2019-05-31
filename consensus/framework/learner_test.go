package framework

import (
	"fmt"
	"github.com/abchain/fabric/consensus/framework/example"
	"github.com/abchain/fabric/core/chaincode"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/ledger/testutil"
	"github.com/abchain/fabric/core/sync"
	"github.com/abchain/fabric/core/sync/strategy"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"testing"
)

func prepareSourceLedger(t *testing.T, l *ledger.Ledger, till int) {

	buildTx := func(i int) []*pb.TransactionHandlingContext {
		return []*pb.TransactionHandlingContext{prepareContractTxe(10 * i),
			prepareContractTxe((-11) * i),
			prepareContractTxe(i + 1)}
	}

	for i := 1; i <= till; i++ {
		txagent, _ := ledger.NewTxEvaluatingAgent(l)
		doneTx, err := chaincode.ExecuteTransactions2(context.Background(), defTestChaincodeSup, buildTx(i), txagent)
		testutil.AssertNoError(t, err, fmt.Sprintf("build block %d, step 1", i))

		testutil.AssertEquals(t, len(doneTx)+i, 3+i)

		err = txagent.FullCommit([]byte(fmt.Sprintf("test %d", i)), doneTx)
		testutil.AssertNoError(t, err, fmt.Sprintf("build block %d, step 2", i))
		testutil.AssertEquals(t, txagent.LastCommitPosition(), uint64(i))
		t.Logf("%d@%.16X", txagent.LastCommitPosition(), txagent.LastCommitState())
		err = l.PutTransactions(doneTx)
		testutil.AssertNoError(t, err, fmt.Sprintf("commit tx on %d", i))
	}
}

func test_core(t *testing.T, tf func(*baseLearnerImpl, *ledger.Ledger)) {

	src, endf1 := ledger.InitSoleTestLedger(t)
	defer endf1()

	target, endf2 := ledger.InitSoleTestLedger(t)
	defer endf2()

	ccrelease := prepareChaincodeSupport(t, src, target)
	defer ccrelease()

	prepareSourceLedger(t, src, 48)

	syncCtx, endctx := context.WithCancel(context.Background())
	defer endctx()

	srcstub := sync.GenTestSyncHub(syncCtx, src, sync.DefaultSyncOption())
	targetstub := sync.GenTestSyncHub(syncCtx, target, sync.DefaultSyncOption())

	err, trf := targetstub.ConnectTo2(syncCtx, srcstub, sync.SyncPacketCommHelper)
	testutil.AssertNoError(t, err, "connect stub")

	sync.PushLedgerStatusOfStub(t, context.Background(), srcstub, src.GetLedgerStatus())
	trf()

	//now we can start a learner

	learner := &baseLearnerImpl{
		sync:              syncstrategy.GetSyncStrategyEntry(targetstub.StreamStub),
		ledger:            target,
		txPrehandle:       pb.DefaultTxHandler,
		txSyncDist:        5,
		blockSyncDist:     30,
		stateSyncDist:     1000,
		fullSyncCriterion: 0,
	}
	learner.cache.pendingBlock = make(map[uint64][]*pb.Block)
	linfo, _ := target.GetLedgerInfo()
	if linfo == nil || linfo.GetHeight() == 0 {
		panic("Learner can not work on an uninited ledger (without a genesis block)")
	}
	learner.refhistory.pos = linfo.GetHeight() - 1
	learner.refhistory.hash = linfo.GetCurrentBlockHash()
	go func() {
		for syncCtx.Err() == nil {
			trf()
		}
	}()

	tf(learner, src)
}

//additional hander to accept raw tx
func (l *baseLearnerImpl) putRawTx(ctx context.Context, tx *pb.Transaction) (*pb.TransactionHandlingContext, error) {

	txe, err := l.txPrehandle.Handle(pb.NewTransactionHandlingContext(tx))
	if err != nil {
		return nil, err
	}

	return txe, l.Put(ctx, txe)
}

func Test_BlockForward(t *testing.T) {

	test_core(t, func(learner *baseLearnerImpl, src *ledger.Ledger) {

		refblk, err := src.GetBlockByNumber(48)
		testutil.AssertNoError(t, err, "ref block")

		cstx := framework_example.BuildTransaction(48, refblk)
		_, err = learner.putRawTx(context.Background(), cstx)
		testutil.AssertNoError(t, err, "put cstx")
		testutil.AssertEquals(t, learner.Ledger().GetBlockchainSize(), uint64(1))
		testutil.AssertEquals(t, len(learner.cache.pendingBlock), 1)

		linfo, err := learner.ledger.GetLedgerInfo()
		testutil.AssertNoError(t, err, "ledger info")

		err = learner.blockForward(context.Background(), linfo)
		testutil.AssertNoError(t, err, "block forward")
		testutil.AssertEquals(t, learner.Ledger().GetBlockchainSize(), uint64(48))
		testutil.AssertEquals(t, len(learner.cache.pendingBlock), 1)
	})
}

func Test_TriggerForward(t *testing.T) {

	test_core(t, func(learner *baseLearnerImpl, src *ledger.Ledger) {

		learner.blockSyncDist = 10
		refblk, err := src.GetBlockByNumber(12)
		testutil.AssertNoError(t, err, "ref block")

		cstx := framework_example.BuildTransaction(12, refblk)
		_, err = learner.putRawTx(context.Background(), cstx)
		testutil.AssertNoError(t, err, "put cstx")

		doProg := learner.Trigger(context.Background())
		testutil.AssertEquals(t, doProg, true)

		linfo, err := learner.ledger.GetLedgerInfo()
		testutil.AssertNoError(t, err, "ledger info")

		testutil.AssertEquals(t, len(learner.cache.pendingBlock), 0)
		testutil.AssertEquals(t, linfo.GetHeight(), uint64(13))
		testutil.AssertEquals(t, linfo.Persisted.States, uint64(13))

	})
}

func Test_OutofOrderBlockForward(t *testing.T) {

	test_core(t, func(learner *baseLearnerImpl, src *ledger.Ledger) {

		learner.blockSyncDist = 10

		fillTx := func(n uint64) {
			refblk, err := src.GetBlockByNumber(n)
			testutil.AssertNoError(t, err, "ref block")

			cstx := framework_example.BuildTransaction(n, refblk)
			_, err = learner.putRawTx(context.Background(), cstx)
			testutil.AssertNoError(t, err, "put cstx")
		}

		fillTx(15)
		fillTx(13)
		fillTx(18)
		fillTx(12)
		testutil.AssertEquals(t, len(learner.cache.pendingBlock), 4)
		testutil.AssertEquals(t, learner.refhistory.syncRefpos, uint64(11))
		testutil.AssertEquals(t, learner.refhistory.pos, uint64(17))

		doProg := learner.Trigger(context.Background())
		testutil.AssertEquals(t, doProg, true)

		linfo, err := learner.ledger.GetLedgerInfo()
		testutil.AssertNoError(t, err, "ledger info")

		testutil.AssertEquals(t, len(learner.cache.pendingBlock), 2)
		testutil.AssertEquals(t, linfo.Persisted.States, uint64(14))

		fillTx(32)
		doProg = learner.Trigger(context.Background())
		testutil.AssertEquals(t, doProg, true)
		testutil.AssertEquals(t, len(learner.cache.pendingBlock), 1)

		linfo, err = learner.ledger.GetLedgerInfo()
		testutil.AssertNoError(t, err, "ledger info")

		testutil.AssertEquals(t, linfo.Persisted.States, uint64(19))

		fillTx(16)
		testutil.AssertEquals(t, len(learner.cache.pendingBlock), 1)
		fillTx(21)
		fillTx(20)
		fillTx(19)
		testutil.AssertEquals(t, len(learner.cache.pendingBlock), 1)

		doProg = learner.Trigger(context.Background())
		testutil.AssertEquals(t, doProg, true)
		linfo, err = learner.ledger.GetLedgerInfo()
		testutil.AssertNoError(t, err, "ledger info")
		testutil.AssertEquals(t, linfo.Persisted.States, uint64(22))
	})
}

func Test_FullStateSyncForward(t *testing.T) {
	test_core(t, func(learner *baseLearnerImpl, src *ledger.Ledger) {

		learner.fullSyncCriterion = 2

		refblk, err := src.GetBlockByNumber(35)
		testutil.AssertNoError(t, err, "ref block")

		cstx := framework_example.BuildTransaction(35, refblk)
		_, err = learner.putRawTx(context.Background(), cstx)
		testutil.AssertNoError(t, err, "put cstx")

		doProg := learner.Trigger(context.Background())
		testutil.AssertEquals(t, doProg, true)

		linfo, err := learner.ledger.GetLedgerInfo()
		testutil.AssertNoError(t, err, "ledger info")

		testutil.AssertEquals(t, linfo.GetHeight(), uint64(36))
		testutil.AssertEquals(t, linfo.Persisted.States, uint64(36))
	})
}
