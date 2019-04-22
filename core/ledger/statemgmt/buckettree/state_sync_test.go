package buckettree

import (
	"fmt"
	"testing"

	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/core/ledger/testutil"
)

func TestSyncStepCalc(t *testing.T) {
	testCfg := func(num, group int, delta int, header string) (int, []int) {
		testDBWrapper.CleanDB(t)
		s := newStateImplTestWrapperWithCustomConfig(t, num, group)
		cfg := s.stateImpl.currentConfig
		cfg.syncDelta = delta
		s.stateImpl.InitPartialSync(make([]byte, 32))

		raw, err := s.stateImpl.RequiredParts()
		testutil.AssertNoError(t, err, header)

		first := raw[0].GetBuckettree()
		testutil.AssertNotNil(t, first)

		t.Logf("%s: %v", header, first)

		testutil.AssertEquals(t, first.BucketNum, uint64(1))
		testutil.AssertEquals(t, int(first.BucketNum+first.Delta), cfg.getNumBuckets(int(first.Level))+1)
		if l := len(s.stateImpl.underSync.syncLevels); l == 0 {
			testutil.AssertEquals(t, int(first.Level), cfg.getLowestLevel())
		} else {
			testutil.AssertEquals(t, int(first.Level), s.stateImpl.underSync.syncLevels[l-1])
		}

		return s.stateImpl.underSync.metaDelta, s.stateImpl.underSync.syncLevels

	}

	_, lvl := testCfg(100, 2, 8, "test 1")
	testutil.AssertEquals(t, len(lvl), 1)
	testutil.AssertEquals(t, lvl[0], 3)
	mdelta, lvl := testCfg(100, 4, 5, "test 2")
	testutil.AssertEquals(t, mdelta, 7) //adjusted from 25 to 28, so get 7 (4*7)
	testutil.AssertEquals(t, len(lvl), 2)
	testutil.AssertEquals(t, lvl[0], 3)
	testutil.AssertEquals(t, lvl[1], 2)

	_, lvl = testCfg(100, 2, 128, "test large delta")
	testutil.AssertEquals(t, len(lvl), 0)

	mdelta, lvl = testCfg(100, 2, 101, "test large delta not aligned")
	t.Log(lvl)
	testutil.AssertEquals(t, mdelta, 256) //adjusted from 505 to 512 and get 2*<256>
	testutil.AssertEquals(t, len(lvl), 0)

	mdelta, lvl = testCfg(100, 12, 2, "test small delta (will start syncing on level 0)")
	testutil.AssertEquals(t, len(lvl), 2)
	testutil.AssertEquals(t, mdelta, 1)
	testutil.AssertEquals(t, lvl[1], 0)
}

func prepare(t *testing.T, num, group int) *stateImplTestWrapper {

	db, _ := db.StartDB(testutil.GenerateID(t), nil)

	return newStateImplTestWrapperOnDBWithCustomConfig(t, db, num, group)
}

type iteratorWithSN struct {
	statemgmt.PartialRangeIterator
	sn *db.DBSnapshot
}

func (i *iteratorWithSN) Close() {
	i.sn.Release()
	i.PartialRangeIterator.Close()
}

func start(t *testing.T, fillnum, delta int, src, target *stateImplTestWrapper) *statemgmt.SyncSimulator {

	statemgmt.PopulateStateForTest(t, src.stateImpl, src.stateImpl.OpenchainDB, fillnum)

	simulator := statemgmt.NewSyncSimulator(target.stateImpl.OpenchainDB)

	sn := src.stateImpl.GetSnapshot()
	srci, err := src.stateImpl.GetPartialRangeIterator(sn)
	testutil.AssertNoError(t, err, "partial iterator")

	simulator.AttachSource(&iteratorWithSN{srci, sn})
	simulator.AttachTarget(target.stateImpl)

	target.stateImpl.currentConfig.syncDelta = delta
	target.stateImpl.InitPartialSync(src.computeCryptoHash())

	return simulator
}

func attach(t *testing.T, src, target *stateImplTestWrapper) *statemgmt.SyncSimulator {

	simulator := statemgmt.NewSyncSimulator(target.stateImpl.OpenchainDB)

	sn := src.stateImpl.GetSnapshot()
	srci, err := src.stateImpl.GetPartialRangeIterator(sn)
	testutil.AssertNoError(t, err, "partial iterator")

	simulator.AttachSource(&iteratorWithSN{srci, sn})
	simulator.AttachTarget(target.stateImpl)

	testutil.AssertEquals(t, target.stateImpl.IsCompleted(), false)
	return simulator
}

func finalize(s *stateImplTestWrapper) {
	db.StopDB(s.stateImpl.OpenchainDB)
}

func logMetaOutput(t *testing.T, sim *statemgmt.SyncSimulator) {
	t.Logf("Now log meta chunk: [%v]", sim.SyncingData.GetOffset().GetBuckettree())
	for _, node := range sim.SyncingData.GetMetaData().GetBuckettree().GetNodes() {
		t.Logf("   [%d-%d] %X", node.Level, node.BucketNum, node.CryptoHash)
	}
}

func logDataOutput(t *testing.T, sim *statemgmt.SyncSimulator) {
	t.Logf("Now log delta chunk: [%v]", sim.SyncingData.GetOffset().GetBuckettree())
	for k, v := range sim.SyncingData.GetChaincodeStateDeltas() {
		t.Logf("   %s: %d kvpairs", k, len(v.GetUpdatedKVs()))
	}
}

func logCacheOutput(t *testing.T, s *stateImplTestWrapper, minL, maxL int) {

	for _, node := range s.stateImpl.bucketCache.c {
		if l := node.bucketKey.level; l >= minL && l <= maxL {
			t.Logf("node [%v]: %X", node.bucketKey, node.childrenCryptoHash)
		}
	}
}

func logDeltaOutput(t *testing.T, s *stateImplTestWrapper, minL, maxL int) {

	for l, nodeMap := range s.stateImpl.bucketTreeDelta.byLevel {
		if l >= minL && l <= maxL {
			for _, node := range nodeMap {
				t.Logf("node (delta) [%v]: %X", node.bucketKey, node.childrenCryptoHash)
			}
		}
	}

}

func TestSyncBasic1(t *testing.T) {
	testDBWrapper.CleanDB(t)

	src, target := prepare(t, 100, 2), prepare(t, 100, 2)
	defer finalize(src)
	defer finalize(target)

	sim := start(t, 60, 8, src, target)
	defer sim.Release()

	//first turn, must have nodes as many as target level
	retE := sim.TestSyncEachStep(func() { logDeltaOutput(t, target, 2, 2) })
	logCacheOutput(t, src, 2, 3)
	logMetaOutput(t, sim)

	testutil.AssertNoError(t, retE, "1-1")

	t.Log(sim.PeekTasks(), target.stateImpl.underSync.current)
	//second turn, it was data turn
	retE = sim.TestSyncEachStep()
	t.Log(sim.SyncingOffset)
	logDataOutput(t, sim)
	logCacheOutput(t, target, 3, 3)

	testutil.AssertNil(t, sim.SyncingData.GetMetaData().GetBuckettree())
	testutil.AssertNotNil(t, sim.SyncingData.GetChaincodeStateDeltas())
	testutil.AssertNoError(t, retE, "data-1")

	retE = sim.TestSyncEachStep()
	testutil.AssertNoError(t, retE, "data-2")
}

func TestSyncBasic2(t *testing.T) {
	//with data not aligned
	testDBWrapper.CleanDB(t)

	src, target := prepare(t, 100, 4), prepare(t, 100, 4)
	defer finalize(src)
	defer finalize(target)

	sim := start(t, 60, 3, src, target)
	defer sim.Release()

	//first turn, must have nodes as many as target level
	retE := sim.TestSyncEachStep(func() { logDeltaOutput(t, target, 2, 2) })
	logCacheOutput(t, src, 2, 3)
	logMetaOutput(t, sim)

	testutil.AssertNoError(t, retE, "1-1")

	//second turn, still meta data
	retE = sim.TestSyncEachStep()
	testutil.AssertNotNil(t, sim.SyncingData.GetMetaData().GetBuckettree())
	testutil.AssertNoError(t, retE, "2-1")

	testutil.AssertNoError(t, sim.PullOut(), "pullout")
}

func testSyncLarge_basic(t *testing.T) {
	//large set with iteration must iterate on bucketnode larger than 128
	testDBWrapper.CleanDB(t)

	src, target := prepare(t, 1000, 3), prepare(t, 1000, 3)
	defer finalize(src)
	defer finalize(target)

	//so we have a meta-syncing on lowestlevel - 1, walk 4 buckets each time
	sim := start(t, 350, 2, src, target)
	defer sim.Release()

	//we sync 45 times (1+2+4+10+28) to level 6 (lowest -1) and will encounter buckets larger than 128
	//can we iterate it correctly?
	for i := 0; i < 45; i++ {
		testutil.AssertNoError(t, sim.TestSyncEachStep(), fmt.Sprintf("first syncing step %d", i))
	}

	retE := sim.TestSyncEachStep()
	testutil.AssertEquals(t, int(sim.SyncingOffset.GetBuckettree().GetLevel()), 6)
	logMetaOutput(t, sim)
	testutil.AssertNoError(t, retE, "syncing on lvl 6 step 1")

	retE = sim.PullOut()
	logMetaOutput(t, sim)
	logCacheOutput(t, src, 6, 6)
	testutil.AssertNoError(t, retE, "pullout")
}

func TestSyncLarge(t *testing.T) {
	defencoding := useLegacyBucketKeyEncoding
	useLegacyBucketKeyEncoding = true
	testSyncLarge_basic(t)
	useLegacyBucketKeyEncoding = defencoding
}

func TestSyncLarge_Legacy(t *testing.T) {
	defencoding := useLegacyBucketKeyEncoding
	useLegacyBucketKeyEncoding = false
	testSyncLarge_basic(t)

	useLegacyBucketKeyEncoding = defencoding
}

func TestSyncBucketNodeEncodeCompatible(t *testing.T) {
	//large set with iteration must iterate on bucketnode larger than 128
	testDBWrapper.CleanDB(t)

	defencoding := useLegacyBucketKeyEncoding
	useLegacyBucketKeyEncoding = false
	src := prepare(t, 1000, 3)
	useLegacyBucketKeyEncoding = true
	target := prepare(t, 1000, 3)
	useLegacyBucketKeyEncoding = defencoding
	defer finalize(src)
	defer finalize(target)

	testutil.AssertEquals(t, src.stateImpl.currentConfig.newBucketKeyEncoding, true)
	testutil.AssertEquals(t, target.stateImpl.currentConfig.newBucketKeyEncoding, false)

	sim := start(t, 350, 2, src, target)
	defer sim.Release()

	testutil.AssertNoError(t, sim.PullOut(), "pullout")
}

func TestSyncResuming(t *testing.T) {
	//large set with iteration must iterate on bucketnode larger than 128
	testDBWrapper.CleanDB(t)

	src, target := prepare(t, 1000, 3), prepare(t, 1000, 3)
	defer finalize(src)
	defer finalize(target)

	sim1 := start(t, 350, 2, src, target)
	defer sim1.Release()

	//phase 1, part of metadata (we need 45 transfering to level 6)
	for i := 0; i < 25; i++ {
		testutil.AssertNoError(t, sim1.TestSyncEachStep(), "sync phase 1")
	}

	dummyWrapper := &stateImplTestWrapper{map[string]interface{}{}, nil, t}
	dummyWrapper.stateImpl = NewStateImpl(target.stateImpl.OpenchainDB)
	err := dummyWrapper.stateImpl.Initialize(dummyWrapper.configMap)
	testutil.AssertError(t, err, "should be inprocess error")
	if _, ok := err.(statemgmt.SyncInProgress); !ok {
		t.Fatalf("unexpect type of error [%#v]", err)
	}
	testutil.AssertEquals(t, dummyWrapper.stateImpl.IsCompleted(), false)
	testutil.AssertEquals(t, dummyWrapper.stateImpl.SyncTarget(), src.computeCryptoHash())

	sim2 := attach(t, src, dummyWrapper)
	defer sim2.Release()

	verifyLevel := func(s *StateImpl, expectedlvl int) {
		raw, err := s.RequiredParts()
		testutil.AssertNoError(t, err, "peek task")

		first := raw[0].GetBuckettree()
		testutil.AssertNotNil(t, first)

		testutil.AssertEquals(t, int(first.GetLevel()), expectedlvl)
	}

	verifyLevel(dummyWrapper.stateImpl, 5)
	//phase 2, finish metadata and start data transfer (require ~130 transfers)
	for i := 0; i < 130; i++ {
		testutil.AssertNoError(t, sim2.TestSyncEachStep(), "sync phase 2")
	}

	dummyWrapper.stateImpl = NewStateImpl(target.stateImpl.OpenchainDB)
	err = dummyWrapper.stateImpl.Initialize(dummyWrapper.configMap)
	testutil.AssertError(t, err, "should be inprocess error")

	sim3 := attach(t, src, dummyWrapper)
	defer sim3.Release()

	verifyLevel(dummyWrapper.stateImpl, 7)
	testutil.AssertNoError(t, sim3.PullOut(), "pullout")
}

func TestSyncFlaw1(t *testing.T) {

	//when we have missed a whole metadata deliberately
	testDBWrapper.CleanDB(t)

	src, target := prepare(t, 100, 4), prepare(t, 100, 4)
	defer finalize(src)
	defer finalize(target)

	//fill more keys so metadata table become full enough
	sim := start(t, 200, 3, src, target)
	defer sim.Release()

	testutil.AssertNoError(t, sim.TestSyncEachStep(), "first meta")

	testutil.AssertNoError(t, sim.TestSyncEachStep(), "meta sync 2-1")

	//test clean the whole metadata ...
	retE := sim.TestSyncEachStep_Taskphase().TestSyncEachStep_Pollphase().Result()
	testutil.AssertNoError(t, retE, "meta sync 2-2-1")

	data := sim.SyncingData.MetaData.GetBuckettree()
	//rarely case ...
	if len(data.Nodes) == 0 {
		t.Log("initial data has empty metadata here, fail and try again")
		t.FailNow()
	}
	//clean all nodes
	data.Nodes = data.Nodes[:0]
	sim.TestSyncEachStep_Applyphase().TestSyncEachStep_Finalphase()
	testutil.AssertError(t, sim.SyncingError, "meta sync 2-2-1-apply")
	t.Log("Obtain expected error:", sim.SyncingError)

	//and we must be able to resume from error
	testutil.AssertNoError(t, sim.PullOut(), "pullout")
}

func TestSyncFlaw2(t *testing.T) {

	//when we have a "replay" attacking
	testDBWrapper.CleanDB(t)

	src, target := prepare(t, 100, 4), prepare(t, 100, 4)
	defer finalize(src)
	defer finalize(target)

	//fill more keys so metadata table become full enough
	sim := start(t, 200, 3, src, target)
	defer sim.Release()

	testutil.AssertNoError(t, sim.TestSyncEachStep(), "first meta")

	testutil.AssertNoError(t, sim.TestSyncEachStep(), "meta sync 2-1")
	replayedData := sim.SyncingData

	//test clean the whole metadata ...
	retE := sim.TestSyncEachStep_Taskphase().TestSyncEachStep_Pollphase().Result()
	testutil.AssertNoError(t, retE, "meta sync 2-2-1")

	sim.SyncingData = replayedData
	sim.TestSyncEachStep_Applyphase().TestSyncEachStep_Finalphase()
	testutil.AssertError(t, sim.SyncingError, "meta sync 2-2-1-apply")
	t.Log("Obtain expected error:", sim.SyncingError)

	//and we must be able to resume from error
	for i := 0; i < 8; i++ {
		testutil.AssertNoError(t, sim.TestSyncEachStep(), "finish meta sync")
	}

	replayedData = sim.SyncingData
	if len(replayedData.ChaincodeStateDeltas) == 0 {
		t.Log("initial data has empty delta here, fail and try again")
		t.FailNow()
	}

	sim.TestSyncEachStep_Taskphase().TestSyncEachStep_Pollphase()
	sim.SyncingData = replayedData
	sim.TestSyncEachStep_Applyphase().TestSyncEachStep_Finalphase()
	testutil.AssertError(t, sim.SyncingError, "data sync replay")
	t.Log("Obtain expected error:", sim.SyncingError)

	testutil.AssertNoError(t, sim.PullOut(), "pullout")
}

func TestSyncFlaw3(t *testing.T) {

	//simply change the value in a k-v pair of delta
	testDBWrapper.CleanDB(t)

	src, target := prepare(t, 100, 4), prepare(t, 100, 4)
	defer finalize(src)
	defer finalize(target)

	//fill more keys so metadata table become full enough
	sim := start(t, 200, 3, src, target)
	defer sim.Release()

	//and we must be able to resume from error
	for i := 0; i < 10; i++ {
		testutil.AssertNoError(t, sim.TestSyncEachStep(), "finish first phase")
	}

	sim.TestSyncEachStep_Taskphase().TestSyncEachStep_Pollphase()

	for _, cc := range sim.SyncingData.ChaincodeStateDeltas {
		for _, v := range cc.GetUpdatedKVs() {
			if v.ValueWrap == nil {
				continue
			}

			v.ValueWrap.Value = []byte("Wrong value")

			sim.TestSyncEachStep_Applyphase().TestSyncEachStep_Finalphase()
			t.Log("Obtain expected error:", sim.SyncingError)

			testutil.AssertNoError(t, sim.PullOut(), "pullout")

			return
		}
	}

	t.Log("data has empty delta, fail and try again")
	t.FailNow()
}
