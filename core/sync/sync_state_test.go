package sync

import (
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/core/ledger/testutil"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"testing"
	"time"
)

func prepareStateForLedger(t *testing.T, l *ledger.Ledger, datakeys int) {

	//add gensis block
	l.BeginTxBatch(0)
	l.CommitTxBatch(0, nil, nil, nil)

	delta := statemgmt.ConstructRandomStateDelta(t, "", 8, 125*datakeys, datakeys, 32)
	cid := "bigTx"
	err := l.ApplyStateDelta(cid, delta)
	testutil.AssertNoError(t, err, "apply delta")
	err = l.CommitTxBatch(cid, nil, nil, []byte("great tx"))
	testutil.AssertNoError(t, err, "commit tx")
}

//notice test.yaml: we set bucketkey with 100 buckets
const populateKeys = 80

func TestStateSync_Basic(t *testing.T) {

	testLedger := ledger.InitTestLedger(t)
	prepareStateForLedger(t, testLedger, populateKeys)

	targetHash, err := testLedger.GetCurrentStateHash()
	testutil.AssertNoError(t, err, "get statehash")

	//notice current state can be used for state syncing
	sn, err := testLedger.CreateSyncingSnapshot(targetHash)
	testutil.AssertNoError(t, err, "check syncing state")
	sn.Release()

	targetLedger, endf := ledger.InitSecondaryTestLedger(t)
	defer endf()

	testCtx, endTest := context.WithCancel(context.Background())
	defer endTest()

	handler := newSyncHandler(testCtx, &pb.PeerID{Name: "test1"}, testLedger, DefaultSyncOption())

	cliCore := newFsmHandler()
	cliCore.sessionOpt.maxWindow = DefaultSyncOption().baseSessionWindow

	syncer, err := ledger.NewSyncAgent(targetLedger, 0, targetHash)
	testutil.AssertNoError(t, err, "create syncer")

	stateCli, endSyncF := NewStateSyncClient(testCtx, syncer)
	defer endSyncF()
	dummyMsg, endDF := NewReceiver(handler.core.syncCore, cliCore, nil)
	defer endDF()
	err = stateCli.handlingFunc(testCtx, 1, dummyMsg, cliCore)
	testutil.AssertNoError(t, filterErr(err), "Full syncing")

	err = syncer.FinishTask()
	testutil.AssertNoError(t, err, "sync task finish")
}

func TestStateSync_FullSimu(t *testing.T) {

	testLedger := ledger.InitTestLedger(t)
	prepareStateForLedger(t, testLedger, populateKeys)
	fillBlocks(t, testLedger, 60)

	targetLedger, endf1 := ledger.InitSecondaryTestLedger(t)
	defer endf1()

	dummyLedger, endf2 := ledger.InitSecondaryTestLedger(t)
	defer endf2()

	baseCtx, endAll := context.WithCancel(context.Background())
	defer endAll()
	baseOpt := DefaultSyncOption()

	testSrc := &testFactory{baseCtx, testLedger, baseOpt}
	testTarget := &testFactory{baseCtx, targetLedger, baseOpt}
	testDummy := &testFactory{baseCtx, dummyLedger, baseOpt}

	peerSrc := testSrc.preparePeer("src")
	peerTarget := testTarget.preparePeer("target")
	peerDummy1, peerDummy2 := testDummy.preparePeer("dummy1"), testDummy.preparePeer("dummy2")

	err, trf1 := peerTarget.ConnectTo2(baseCtx, peerSrc, packageMsgHelper)
	testutil.AssertNoError(t, err, "conn 1")

	err, trf2 := peerTarget.ConnectTo2(baseCtx, peerDummy1, packageMsgHelper)
	testutil.AssertNoError(t, err, "conn 2")

	err, trf3 := peerTarget.ConnectTo2(baseCtx, peerDummy2, packageMsgHelper)
	testutil.AssertNoError(t, err, "conn 3")

	//start traffic ...
	func(trfs ...func() error) {
		for i, trf := range trfs {
			go func(i int, f func() error) {
				for baseCtx.Err() == nil {
					err := f()
					if err != nil {
						t.Logf("do a traffic on %d: %s", i, err)
					}
				}
			}(i, trf)
		}
	}(trf1, trf2, trf3)

	srcState := testLedger.GetLedgerStatus()
	//manual do pushing ...
	for strm := range peerSrc.OverAllHandlers(baseCtx) {
		castedh := strm.Impl().(*syncHandler)
		err := castedh.PushLocalLedgerState(strm.StreamHandler, srcState)
		testutil.AssertNoError(t, err, "state pushing")
	}

	//must wait until push is worked (msg is passed via many channels)
	time.Sleep(100 * time.Millisecond)

	pp := peerTarget.PickHandler(&pb.PeerID{Name: "src"})
	testutil.AssertNotNil(t, pp)
	rst := pp.Impl().(*syncHandler).GetRemoteLedgerState()
	testutil.AssertNotNil(t, rst)
	testutil.AssertEquals(t, rst.GetHeight(), uint64(62))

	syncTargetHeight := uint64(61)
	//now let's try sync blocks!
	blkAgent := ledger.NewBlockAgent(targetLedger)
	blockCli := NewBlockSyncClient(blkAgent.SyncCommitBlock,
		BlocSyncSimplePlan(targetLedger, syncTargetHeight, getBlockInfo(t, testLedger, syncTargetHeight)))

	err = ExecuteSyncTask(baseCtx, blockCli, peerTarget.StreamStub)
	testutil.AssertNoError(t, err, "block syncing")
	testutil.AssertEquals(t, targetLedger.GetBlockchainSize(), syncTargetHeight+1)
	testBlocks(t, testLedger, targetLedger, 60)

	//then, states!
	sdetector := NewStateSyncDetector(targetLedger, 40)
	err = sdetector.DoDetection(peerTarget.StreamStub)
	testutil.AssertNoError(t, err, "s detection")
	testutil.AssertNotNil(t, sdetector.Candidate.State)
	t.Logf("s [%X] height %d", sdetector.Candidate.State, sdetector.Candidate.Height)
	if sdetector.Candidate.Height < 40 {
		t.Fatalf("Candidate state is too low: %d", sdetector.Candidate.Height)
	}

	syncer, err := ledger.NewSyncAgent(targetLedger, sdetector.Candidate.Height, sdetector.Candidate.State)
	testutil.AssertNoError(t, err, "create syncer")

	stateCli, endSyncF := NewStateSyncClient(baseCtx, syncer)
	defer endSyncF()

	err = ExecuteSyncTask(baseCtx, stateCli, peerTarget.StreamStub)
	testutil.AssertNoError(t, err, "state syncing")

	err = syncer.FinishTask()
	testutil.AssertNoError(t, err, "sync task finish")

	finalS, err := targetLedger.GetCurrentStateHash()
	testutil.AssertNoError(t, err, "get state")
	testutil.AssertEquals(t, finalS, sdetector.Candidate.State)

	sheight, err := testLedger.GetBlockNumberByState(finalS)
	testutil.AssertNoError(t, err, "get bkn")
	testutil.AssertEquals(t, sheight, sdetector.Candidate.Height)
}
