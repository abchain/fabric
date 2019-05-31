package framework

import (
	"github.com/abchain/fabric/consensus/framework/example"
	cspb "github.com/abchain/fabric/consensus/protos"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/ledger/testutil"
	"github.com/abchain/fabric/events/litekfk"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"testing"
	"time"
)

func Test_MainRoutineBase(t *testing.T) {

	workTopic := litekfk.CreateTopic(litekfk.NewDefaultConfig())

	cbase := NewConsensusBase(workTopic)
	cbase.triggerTime = 1 * time.Second
	runEnd := make(chan interface{})

	test_core(t, func(learner *baseLearnerImpl, src *ledger.Ledger) {

		runCtx, endf := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer endf()

		refblk, err := src.GetBlockByNumber(48)
		testutil.AssertNoError(t, err, "ref block")

		cstx := framework_example.BuildTransaction(48, refblk)
		cstxe, err := learner.txPrehandle.Handle(pb.NewTransactionHandlingContext(cstx))
		testutil.AssertNoError(t, err, "build txe")

		go func() {
			cbase.MainRoutine(runCtx, learner)
			close(runEnd)
		}()

		cbase.immediateH <- cstxe
		<-runEnd

		linfo, err := learner.ledger.GetLedgerInfo()
		testutil.AssertNoError(t, err, "ledger info")
		testutil.AssertEquals(t, linfo.Persisted.States, uint64(49))
	})
}

func Test_MainRoutine1(t *testing.T) {

	workTopic := litekfk.CreateTopic(litekfk.NewDefaultConfig())

	cbase := NewConsensusBase(workTopic)
	cbase.triggerTime = 1 * time.Second
	runEnd := make(chan interface{})

	test_core(t, func(learner *baseLearnerImpl, src *ledger.Ledger) {

		buildTxe := func(n uint64) *pb.TransactionHandlingContext {
			refblk, err := src.GetBlockByNumber(n)
			testutil.AssertNoError(t, err, "ref block")

			cstx := framework_example.BuildTransaction(n, refblk)
			cstxe, err := learner.txPrehandle.Handle(pb.NewTransactionHandlingContext(cstx))
			testutil.AssertNoError(t, err, "build txe")
			return cstxe
		}

		workTopic.Write(buildTxe(12))
		workTopic.Write(buildTxe(13))
		workTopic.Write(buildTxe(14))
		workTopic.Write(buildTxe(17))

		runCtx, endf := context.WithTimeout(context.Background(), 2000*time.Millisecond)
		defer endf()

		go func() {
			cbase.MainRoutine(runCtx, learner)
			close(runEnd)
		}()

		cbase.immediateH <- buildTxe(33)
		<-runEnd

		linfo, err := learner.ledger.GetLedgerInfo()
		testutil.AssertNoError(t, err, "ledger info")
		testutil.AssertEquals(t, linfo.Persisted.States, uint64(15))
		testutil.AssertEquals(t, len(learner.cache.pendingBlock), 2)

		cbase.immediateH = make(chan *pb.TransactionHandlingContext, 3)
		cbase.immediateH <- buildTxe(16)
		cbase.immediateH <- buildTxe(15)
		runCtx, endf = context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer endf()

		cbase.MainRoutine(runCtx, learner)

		linfo, err = learner.ledger.GetLedgerInfo()
		testutil.AssertNoError(t, err, "ledger info")
		testutil.AssertEquals(t, linfo.Persisted.States, uint64(18))

	})

}

type dummyLearner struct {
	end        context.CancelFunc
	counter    int
	triggercnt int
}

func (dl *dummyLearner) Preview(context.Context, uint64, []*pb.TransactionHandlingContext) (*pb.Block, error) {
	panic("DONT CALL")
}
func (dl *dummyLearner) PreviewSimple([]*pb.TransactionHandlingContext) *pb.Block {
	panic("DONT CALL")
}
func (dl *dummyLearner) Ledger() *ledger.Ledger {
	panic("DONT CALL")
}
func (dl *dummyLearner) HistoryTop() (uint64, []byte) {
	panic("DONT CALL")
}
func (dl *dummyLearner) Trigger(context.Context) bool {
	switch dl.triggercnt {
	case 0:
		dl.triggercnt++
		return true
	case 1:
		dl.triggercnt++
		return false
	case 2:
		dl.triggercnt++
		return true
	default:
		dl.end()
		return true
	}
}
func (dl *dummyLearner) Put(context.Context, *pb.TransactionHandlingContext) error {
	if dl.triggercnt == 2 {
		panic("wrong state")
	}
	dl.counter++
	return ErrorWriteBack{}
}

func Test_MainRoutineWriteBack(t *testing.T) {

	workTopic := litekfk.CreateTopic(litekfk.NewDefaultConfig())

	cbase := NewConsensusBase(workTopic)
	cbase.triggerTime = 1 * time.Second

	runCtx, endf := context.WithCancel(context.Background())

	dummyTx, _ := pb.NewTransaction(pb.ChaincodeID{Name: "dummy"}, "testing", "", nil)
	workTopic.Write(pb.NewTransactionHandlingContext(dummyTx))
	workTopic.Write(pb.NewTransactionHandlingContext(dummyTx))
	workTopic.Write(pb.NewTransactionHandlingContext(dummyTx))

	dl := &dummyLearner{end: endf}
	cbase.MainRoutine(runCtx, dl)

	//for each time we come into writeback cycle, we must walk through 3 tx
	//and we forward 3 times, each time handle one tx, and totally 3x3+3 = 12 times
	testutil.AssertEquals(t, dl.counter, 12)
}

type dummyPurposer bool

func (b dummyPurposer) RequireState() bool { return bool(b) }
func (dummyPurposer) Purpose(blk *cspb.PurposeBlock) *cspb.ConsensusPurpose {
	return &cspb.ConsensusPurpose{
		Out: &cspb.ConsensusPurpose_Txs{
			Txs: &pb.TransactionBlock{
				Transactions: []*pb.Transaction{framework_example.BuildTransaction(blk.N, blk.B)},
			},
		},
	}
}
func (dummyPurposer) Cancel() {}

type dummyDeliver func([]*pb.Transaction)

func (f dummyDeliver) Deliver(_ context.Context, txs []*pb.Transaction) error {
	f(txs)
	return nil
}

func Test_PurposingBase(t *testing.T) {

	workTopic := litekfk.CreateTopic(litekfk.NewDefaultConfig())

	txTopic := []litekfk.Topic{
		litekfk.CreateTopic(litekfk.NewDefaultConfig()),
		litekfk.CreateTopic(litekfk.NewDefaultConfig()),
	}

	cbase := NewConsensusBase(workTopic)
	cbase.triggerTime = 1 * time.Second
	runEnd := make(chan interface{})

	test_core(t, func(learner *baseLearnerImpl, src *ledger.Ledger) {

		turnTxe := func(txs []*pb.Transaction) (ret []*pb.TransactionHandlingContext) {
			for _, tx := range txs {
				txe, err := learner.txPrehandle.Handle(pb.NewTransactionHandlingContext(tx))
				testutil.AssertNoError(t, err, "build txe")

				ret = append(ret, txe)
			}
			return
		}

		purpose := cbase.BuildBasePurposerRoutine(dummyPurposer(true),
			dummyDeliver(func(txs []*pb.Transaction) {
				for _, txe := range turnTxe(txs) {
					cbase.immediateH <- txe
				}
			}), 5, txTopic[0].NewClient(), txTopic[1].NewClient())

		refblk, err := src.GetBlockByNumber(1)
		testutil.AssertNoError(t, err, "ref block")

		txes := turnTxe(refblk.GetTransactions())
		txTopic[0].Write(txes[0])
		txTopic[0].Write(txes[2])
		txTopic[1].Write(txes[1])

		wait := purpose()

		runCtx, endf := context.WithTimeout(context.Background(), 1000*time.Millisecond)
		defer endf()

		go func() {
			cbase.MainRoutine(runCtx, learner)
			close(runEnd)
		}()

		testutil.AssertNoError(t, wait(context.Background()), "purpose result")
		<-runEnd

		linfo, err := learner.ledger.GetLedgerInfo()
		testutil.AssertNoError(t, err, "ledger info")
		testutil.AssertEquals(t, linfo.Persisted.States, uint64(2))

		testutil.AssertEquals(t, linfo.GetCurrentStateHash(), refblk.GetStateHash())

		refblk2, err := src.GetBlockByNumber(2)
		testutil.AssertNoError(t, err, "ref block")

		refblk3, err := src.GetBlockByNumber(3)
		testutil.AssertNoError(t, err, "ref block")

		txes = turnTxe(append(refblk2.GetTransactions(), refblk3.GetTransactions()...))
		txTopic[0].Write(txes[0])
		txTopic[0].Write(txes[1])
		txTopic[0].Write(txes[2])
		txTopic[1].Write(txes[3])
		txTopic[1].Write(txes[4])
		txTopic[1].Write(txes[5])

		wait = purpose()

		runCtx, endf = context.WithTimeout(context.Background(), 1000*time.Millisecond)
		defer endf()

		runEnd = make(chan interface{})
		go func() {
			cbase.MainRoutine(runCtx, learner)
			close(runEnd)
		}()

		testutil.AssertNoError(t, wait(context.Background()), "purpose result")
		<-runEnd

		linfo, err = learner.ledger.GetLedgerInfo()
		testutil.AssertNoError(t, err, "ledger info")
		testutil.AssertEquals(t, linfo.Persisted.States, uint64(3))

		outblk, err := learner.ledger.GetBlockByNumber(2)
		testutil.AssertEquals(t, len(outblk.GetTransactions()), 5)

	})

}

type failPurposer struct {
	failit bool
	dummyPurposer
}

func (fp *failPurposer) Purpose(blk *cspb.PurposeBlock) *cspb.ConsensusPurpose {
	if fp.failit {
		return &cspb.ConsensusPurpose{
			Out: &cspb.ConsensusPurpose_Error{Error: "just fail"},
		}
	} else {
		return fp.dummyPurposer.Purpose(blk)
	}

}

func Test_PurposingFail(t *testing.T) {

	workTopic := litekfk.CreateTopic(litekfk.NewDefaultConfig())

	txTopic := []litekfk.Topic{
		litekfk.CreateTopic(litekfk.NewDefaultConfig()),
		litekfk.CreateTopic(litekfk.NewDefaultConfig()),
	}

	cbase := NewConsensusBase(workTopic)
	cbase.triggerTime = 1 * time.Second
	runEnd := make(chan interface{})

	test_core(t, func(learner *baseLearnerImpl, src *ledger.Ledger) {

		turnTxe := func(txs []*pb.Transaction) (ret []*pb.TransactionHandlingContext) {
			for _, tx := range txs {
				txe, err := learner.txPrehandle.Handle(pb.NewTransactionHandlingContext(tx))
				testutil.AssertNoError(t, err, "build txe")

				ret = append(ret, txe)
			}
			return
		}

		purposerFailable := &failPurposer{true, dummyPurposer(false)}

		purpose := cbase.BuildBasePurposerRoutine(purposerFailable,
			dummyDeliver(func(txs []*pb.Transaction) {
				for _, txe := range turnTxe(txs) {
					cbase.immediateH <- txe
				}
			}), 5, txTopic[0].NewClient(), txTopic[1].NewClient())

		refblk, err := src.GetBlockByNumber(1)
		testutil.AssertNoError(t, err, "ref block")

		txes := turnTxe(refblk.GetTransactions())
		txTopic[0].Write(txes[0])
		txTopic[0].Write(txes[2])
		txTopic[1].Write(txes[1])

		wait := purpose()

		runCtx, endf := context.WithTimeout(context.Background(), 1000*time.Millisecond)
		defer endf()

		go func() {
			cbase.MainRoutine(runCtx, learner)
			close(runEnd)
		}()

		testutil.AssertError(t, wait(context.Background()), "purpose fail result")
		<-runEnd

		runCtx, endf = context.WithTimeout(context.Background(), 1000*time.Millisecond)
		defer endf()

		runEnd = make(chan interface{})
		go func() {
			cbase.MainRoutine(runCtx, learner)
			close(runEnd)
		}()

		purposerFailable.failit = false
		wait = purpose()
		testutil.AssertNoError(t, wait(context.Background()), "purpose result")
		<-runEnd

		linfo, err := learner.ledger.GetLedgerInfo()
		testutil.AssertNoError(t, err, "ledger info")
		testutil.AssertEquals(t, linfo.Persisted.States, uint64(2))
		testutil.AssertEquals(t, linfo.GetCurrentStateHash(), refblk.GetStateHash())
	})
}
