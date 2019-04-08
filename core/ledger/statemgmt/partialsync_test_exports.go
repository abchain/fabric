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
	if s.src == t {
		return
	} else if s.src != nil {
		s.src.Close()
	}
	s.src = t
}

func (s *SyncSimulator) AttachTarget(t HashAndDividableState) {
	s.target = t
}

func (s *SyncSimulator) Release() {
	if s.src != nil {
		s.src.Close()
	}
}

func (s *SyncSimulator) PeekTasks() []*protos.SyncOffset {
	return s.cachingTasks
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

func (s *SyncSimulator) TestSyncEachStep(task *protos.SyncOffset, onFinish ...func()) (e error) {

	s.SyncingOffset = task
	s.SyncingData = nil
	s.SyncingError = nil
	defer func() {

		for _, f := range onFinish {
			f()
		}

		e = s.SyncingError
		if e == nil {
			s.target.ClearWorkingSet(true)
		} else {
			s.target.ClearWorkingSet(false)
		}
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

func (s *SyncSimulator) PullOut(onTask ...func()) error {

	for tsk := s.PollTask(); tsk != nil; tsk = s.PollTask() {
		s.TestSyncEachStep(tsk, onTask...)
		if s.SyncingError != nil {
			return s.SyncingError
		}
	}

	return nil
}

//populate a moderate size of state collection for testing
func PopulateStateForTest(t testing.TB, target HashAndDividableState, db *db.OpenchainDB, datakeys int) {

	//notice in ConstructRandomStateDelta the arg. "maxKeySuffix" do not indicate the bytelength but the
	//max decimal value of keysuffix (which must be an integer), so we must provide many possible value
	//to made the key random enough, here we always make the possiblily of key is 1000 times of required
	//datakeys (8 for chaincode and 125*datakeys for keys)

	err := target.PrepareWorkingSet(ConstructRandomStateDelta(t, "", 8, 125*datakeys, datakeys, 32))
	testutil.AssertNoError(t, err, "populate state")

	wb := db.NewWriteBatch()
	defer wb.Destroy()

	err = target.AddChangesForPersistence(wb)
	testutil.AssertNoError(t, err, "write persist")
	err = wb.BatchCommit()
	testutil.AssertNoError(t, err, "commit")
	target.ClearWorkingSet(true)
}

func StartFullSyncTest(t testing.TB, src, target HashAndDividableState, db *db.OpenchainDB) {

	sn := db.GetSnapshot()
	defer sn.Release()
	simulator := NewSyncSimulator(db)

	srci, err := src.GetPartialRangeIterator(sn)
	testutil.AssertNoError(t, err, "create iterator")

	simulator.AttachSource(srci)
	simulator.AttachTarget(target)

	srchash, err := src.ComputeCryptoHash()
	t.Logf("try to sync target hash [%x]", srchash)
	testutil.AssertNoError(t, err, "src hash")

	target.InitPartialSync(srchash)

	err = simulator.PullOut(func() { t.Logf("syncing: <%v> --- <%v>", simulator.SyncingOffset, simulator.SyncingData) })
	testutil.AssertNoError(t, err, "sync finish")
	testutil.AssertEquals(t, target.IsCompleted(), true)

	targethash, err := target.ComputeCryptoHash()
	testutil.AssertNoError(t, err, "target hash")

	testutil.AssertEquals(t, srchash, targethash)
}
