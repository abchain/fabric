package buckettree

import (
	_ "fmt"
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

		first, err := raw[0].Unmarshal2BucketTree()
		testutil.AssertNoError(t, err, "unmarshal "+header)

		t.Logf("%s: %v", header, first)

		testutil.AssertEquals(t, first.BucketNum, uint64(1))
		testutil.AssertEquals(t, int(first.Level) <= cfg.getLowestLevel(), true)
		testutil.AssertEquals(t, int(first.Delta) == cfg.getNumBuckets(int(first.Level)), true)

		return s.stateImpl.underSync.metaDelta, s.stateImpl.underSync.syncLevels

	}

	_, lvl := testCfg(100, 2, 8, "test 1")
	testutil.AssertEquals(t, len(lvl), 1)
	testutil.AssertEquals(t, lvl[0], 4)
	mdelta, lvl := testCfg(100, 4, 5, "test 2")
	testutil.AssertEquals(t, mdelta, 16)
	testutil.AssertEquals(t, len(lvl), 2)
	testutil.AssertEquals(t, lvl[0], 4)
	testutil.AssertEquals(t, lvl[1], 2)

	_, lvl = testCfg(100, 2, 128, "test large delta")
	testutil.AssertEquals(t, len(lvl), 0)
	_, lvl = testCfg(100, 2, 101, "test large delta not aligned")
	t.Log(lvl)
	testutil.AssertEquals(t, len(lvl), 0)
}

func TestSync(t *testing.T) {

	testDBWrapper.CleanDB(t)

	secondaryDB, _ := db.StartDB("secondary", nil)
	defer db.StopDB(secondaryDB)

	srcImpl := newStateImplTestWrapperWithCustomConfig(t, 100, 3)
	statemgmt.PopulateStateForTest(t, srcImpl.stateImpl, testDBWrapper.GetDB(), 60)

	targetImpl := newStateImplTestWrapperOnDBWithCustomConfig(t, secondaryDB, 100, 3)

	simulator := statemgmt.NewSyncSimulator(secondaryDB)

	sn := testDBWrapper.GetDB().GetSnapshot()
	defer sn.Release()

	srci, err := srcImpl.stateImpl.GetPartialRangeIterator(sn)
	testutil.AssertNoError(t, err, "partial iterator")

	simulator.AttachSource(srci)
	simulator.AttachTarget(targetImpl.stateImpl)
}

// func TestSync(t *testing.T) {
// 	testDBWrapper.CleanDB(t)
// 	stateImplTestWrapper := newStateImplTestWrapperWithCustomConfig(t, 100, 2)
// 	stateImpl := stateImplTestWrapper.stateImpl
// 	stateDelta := statemgmt.NewStateDelta()

// 	i := 1
// 	for i <= 100 {
// 		chaincode := fmt.Sprintf("chaincode%d", i)
// 		k := fmt.Sprintf("key%d", i)
// 		v := fmt.Sprintf("value%d", i)
// 		stateDelta.Set(chaincode, k, []byte(v), nil)
// 		i++
// 	}

// 	stateImpl.PrepareWorkingSet(stateDelta)
// 	targetHash := stateImplTestWrapper.computeCryptoHash()
// 	stateImplTestWrapper.persistChangesAndResetInMemoryChanges()

// 	err := stateImplTestWrapper.syncState(targetHash)
// 	testutil.AssertNil(t, err)

// 	localHash := stateImplTestWrapper.computeCryptoHash()
// 	fmt.Printf("Local hash: %x\n", localHash)
// 	fmt.Printf("Target hash: %x\n", targetHash)

// 	testutil.AssertEquals(t, localHash, targetHash)
// }
