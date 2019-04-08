package buckettree

import (
	"fmt"
	"testing"

	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/core/ledger/testutil"
)

func TestSyncStepCalc(t *testing.T) {
	testDBWrapper.CleanDB(t)

	testCfg := func(num, group int, delta int, header string) (int, []int) {
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

func finalize(s *stateImplTestWrapper) {
	db.StopDB(s.stateImpl.OpenchainDB)
}

func logMetaOutput(t *testing.T, sim *statemgmt.SyncSimulator) {
	t.Logf("Now log meta chunk: [%v]", sim.SyncingOffset.GetBuckettree())
	for _, node := range sim.SyncingData.GetMetaData().GetBuckettree().GetNodes() {
		t.Logf("   [%d-%d] %X", node.Level, node.BucketNum, node.CryptoHash)
	}
}

func logDataOutput(t *testing.T, sim *statemgmt.SyncSimulator) {
	t.Logf("Now log delta chunk: [%v]", sim.SyncingOffset.GetBuckettree())
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
	retE := sim.TestSyncEachStep(sim.PollTask(), func() { logDeltaOutput(t, target, 2, 2) })
	logCacheOutput(t, src, 2, 3)
	logMetaOutput(t, sim)

	testutil.AssertNoError(t, retE, "1-1")

	t.Log(sim.PeekTasks(), target.stateImpl.underSync.current)
	tsk := sim.PollTask()
	testutil.AssertNoError(t, sim.SyncingError, "data-1-poll")
	t.Log(tsk)
	//second turn, it was data turn
	retE = sim.TestSyncEachStep(tsk)
	logDataOutput(t, sim)
	logCacheOutput(t, target, 3, 3)

	testutil.AssertNil(t, sim.SyncingData.GetMetaData().GetBuckettree())
	testutil.AssertNotNil(t, sim.SyncingData.GetChaincodeStateDeltas())
	testutil.AssertNoError(t, retE, "data-1")

	retE = sim.TestSyncEachStep(sim.PollTask())
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
	retE := sim.TestSyncEachStep(sim.PollTask(), func() { logDeltaOutput(t, target, 2, 2) })
	logCacheOutput(t, src, 2, 3)
	logMetaOutput(t, sim)

	testutil.AssertNoError(t, retE, "1-1")

	//second turn, still meta data
	tsk := sim.PollTask()
	retE = sim.TestSyncEachStep(tsk)
	testutil.AssertNotNil(t, sim.SyncingData.GetMetaData().GetBuckettree())
	testutil.AssertNoError(t, retE, "2-1")

	testutil.AssertNoError(t, sim.PullOut(), "pullout")
}

func TestSyncLarge(t *testing.T) {
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
		testutil.AssertNoError(t, sim.TestSyncEachStep(sim.PollTask()), fmt.Sprintf("first syncing step %d", i))
	}

	tsk := sim.PollTask()
	retE := sim.TestSyncEachStep(tsk)
	testutil.AssertEquals(t, int(tsk.GetBuckettree().GetLevel()), 6)
	logMetaOutput(t, sim)
	testutil.AssertNoError(t, retE, "syncing on lvl 6 step 1")

	retE = sim.PullOut()
	logMetaOutput(t, sim)
	logCacheOutput(t, src, 6, 6)
	testutil.AssertNoError(t, retE, "pullout")
}
