package statemgmt

import (
	"errors"
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger/testutil"
	"github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
	"testing"
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

func (s *SyncSimulator) pollTask() *protos.SyncOffset {
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

func (s *SyncSimulator) Result() error {
	return s.SyncingError
}

func (s *SyncSimulator) TestSyncEachStep_Taskphase() *SyncSimulator {
	s.SyncingOffset = s.pollTask()
	return s
}

func (s *SyncSimulator) TestSyncEachStep_Pollphase() *SyncSimulator {

	if s.SyncingError != nil {
		return s
	} else if s.SyncingOffset == nil {
		s.SyncingError = errors.New("No task")
		return s
	}

	//simluating the transfer process, the passed data must be marshal/unmarshal
	v := new(protos.SyncOffset)
	bt, _ := proto.Marshal(s.SyncingOffset)
	proto.Unmarshal(bt, v)

	if data, err := GetRequiredParts(s.src, v); err != nil {
		s.SyncingError = err
	} else {
		s.SyncingData = data
	}

	return s
}

func (s *SyncSimulator) TestSyncEachStep_Applyphase() *SyncSimulator {

	if s.SyncingError != nil {
		return s
	} else if s.SyncingData == nil {
		s.SyncingError = errors.New("No data")
		return s
	}

	v := new(protos.SyncStateChunk)
	bt, _ := proto.Marshal(s.SyncingData)
	proto.Unmarshal(bt, v)

	s.target.PrepareWorkingSet(GenUpdateStateDelta(v.ChaincodeStateDeltas))

	if err := s.target.ApplyPartialSync(v); err != nil {
		s.SyncingError = err
		return s
	}

	writeBatch := s.NewWriteBatch()
	defer writeBatch.Destroy()

	if err := s.target.AddChangesForPersistence(writeBatch); err != nil {
		s.SyncingError = err
		return s
	}

	s.SyncingError = writeBatch.BatchCommit()
	return s

}

func (s *SyncSimulator) TestSyncEachStep_Finalphase() {
	if s.SyncingError == nil {
		s.target.ClearWorkingSet(true)
	} else {
		s.target.ClearWorkingSet(false)
	}
}

func (s *SyncSimulator) TestSyncEachStep(onFinish ...func()) (e error) {

	s.SyncingOffset = nil
	s.SyncingData = nil
	s.SyncingError = nil

	s.TestSyncEachStep_Taskphase()
	if s.SyncingOffset == nil {
		return errors.New("No task available")
	}

	if err := s.TestSyncEachStep_Pollphase().Result(); err != nil {
		return err
	}

	defer func() {

		for _, f := range onFinish {
			f()
		}

		s.TestSyncEachStep_Finalphase()
	}()

	if err := s.TestSyncEachStep_Applyphase().Result(); err != nil {
		return err
	}

	return nil
}

func (s *SyncSimulator) PullOut(onTask ...func()) error {

	for s.TestSyncEachStep(onTask...) == nil {

		if s.SyncingError != nil {
			return s.SyncingError
		}
	}

	return nil
}

//populate a moderate size of state collection for testing
func PopulateStateForTest(t testing.TB, target HashableState, db *db.OpenchainDB, datakeys int) {

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
