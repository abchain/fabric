package statemgmt

import (
	"github.com/abchain/fabric/core/db"
	"testing"

	"github.com/abchain/fabric/core/ledger/testutil"
	"github.com/abchain/fabric/protos"
)

type SyncSimulator struct {
	*db.OpenchainDB
	src          PartialRangeIterator
	target       HashAndDividableState
	cachingTasks []*protos.SyncOffset

	SyncingOffset *protos.SyncOffset
	SyncingData   *protos.SyncStateChunk
	SyncingError  error
}

func NewSyncSimulator(db *db.OpenchainDB) *SyncSimulator {
	return &SyncSimulator{OpenchainDB: db}
}

func (s *SyncSimulator) AttachSource(t PartialRangeIterator) {
	s.src = t
}

func (s *SyncSimulator) AttachTarget(t HashAndDividableState) {
	s.target = t
}

func (s *SyncSimulator) PollTask() *protos.SyncOffset {
	if len(s.cachingTasks) == 0 {
		s.cachingTasks, s.SyncingError = s.target.RequiredParts()
	}

	if len(s.cachingTasks) == 0 {
		return nil
	} else {
		out := s.cachingTasks[0]
		s.cachingTasks = s.cachingTasks[1:]
		return out
	}

}

func (s *SyncSimulator) TestSyncEachStep(task *protos.SyncOffset) (e error) {

	s.SyncingOffset = task
	s.SyncingData = nil
	s.SyncingError = nil
	defer func() {
		e = s.SyncingError
	}()

	if data, err := GetRequiredParts(s.src, task); err != nil {
		s.SyncingError = err
		return
	} else {
		s.SyncingData = data
	}

	s.target.PrepareWorkingSet(GenUpdateStateDelta(s.SyncingData.ChaincodeStateDeltas))

	if err := s.target.ApplyPartialSync(s.SyncingData); err != nil {
		s.SyncingError = err
		return
	}

	writeBatch := s.NewWriteBatch()
	defer writeBatch.Destroy()

	if err := s.target.AddChangesForPersistence(writeBatch); err != nil {
		s.SyncingError = err
		return
	}

	s.SyncingError = writeBatch.BatchCommit()
	return
}

//populate a moderate size of state collection for testing
func PopulateStateForTest(t testing.TB, target HashAndDividableState, datakeys int) {

	err := target.PrepareWorkingSet(ConstructRandomStateDelta(t, "", 4, 8, datakeys, 32))
	testutil.AssertNoError(t, err, "populate state")
}

func StartFullSyncTest(t testing.TB, src, target HashAndDividableState, db *db.OpenchainDB) {

	sn := db.GetSnapshot()
	defer sn.Release()
	simulator := NewSyncSimulator(db)

	srci, err := src.GetPartialRangeIterator(sn)
	testutil.AssertNoError(t, err, "create iterator")

	simulator.AttachSource(srci)
	simulator.AttachTarget(target)

	for tsk := simulator.PollTask(); tsk != nil; tsk = simulator.PollTask() {
		simulator.TestSyncEachStep(tsk)
		t.Logf("syncing: <%v> --- <%v>", simulator.SyncingOffset, simulator.SyncingData)
		testutil.AssertNoError(t, simulator.SyncingError, "sync step")
	}

	testutil.AssertEquals(t, target.IsCompleted(), true)

	srchash, err := src.ComputeCryptoHash()
	testutil.AssertNoError(t, err, "src hash")
	targethash, err := target.ComputeCryptoHash()
	testutil.AssertNoError(t, err, "target hash")

	testutil.AssertEquals(t, srchash, targethash)
}
