package ledger

import (
	"fmt"
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/core/ledger/statemgmt/state"
	"github.com/abchain/fabric/core/util"
	"github.com/abchain/fabric/protos"
	"github.com/op/go-logging"
)

type TxExecStates struct {
	*state.TxExecStates
}

func NewQueryExecState(l *Ledger) TxExecStates {
	return TxExecStates{state.NewExecStates(l.state.cache.deltas...)}
}

//----------- leagacy API following ------------
func (s *TxExecStates) InitForInvoking(l *Ledger) {
	s.TxExecStates = state.NewExecStatesFromState(l.state.Wrapper(), l.state.cache.deltas...)
}

func (s *TxExecStates) InitForQuerying(l *Ledger) {
	l.readCache.RLock()
	defer l.readCache.RUnlock()
	s.TxExecStates = state.NewExecStates(l.state.cache.deltas...)
}

func (s TxExecStates) AppyInvokingResult(l *Ledger) {
	if s.IsEmpty() {
		return
	}

	l.state.ApplyTxExec(s.TxExecStates)
}

//----------- leagacy API above ------------

func (s TxExecStates) Uninited() bool {
	return s.TxExecStates == nil
}

func (s TxExecStates) IsEmpty() bool {
	return s.TxExecStates == nil || s.TxExecStates.IsEmpty()
}

func (s TxExecStates) Get(chaincodeID string, key string, l *Ledger) (ret []byte, err error) {
	return s.TxExecStates.Get(chaincodeID, key, l.state.GetImpl())
}

func (s TxExecStates) GetRangeScanIterator(chaincodeID string, startKey string, endKey string, l *Ledger) (statemgmt.RangeScanIterator, error) {
	return s.TxExecStates.GetRangeScanIterator(chaincodeID, startKey, endKey, l.state.GetImpl())
}

var statelogger = logging.MustGetLogger("ledger/state")
var genesisHash = []byte("YAFABRIC09_GENSISHASH")

//we rewrite a new light-weight wrapper of states and delta with updated API, like blockchain
//and left the old state object for some dirty implements
type stateWrapper struct {
	*state.State
	configCached        state.StateConfig
	updatesubscriptions []LedgerNewStateNotify
	//notice the implement of hashablestate has a opposite nature against block-building:
	//in most time its in-memory states is different from the db state (by appling
	//PrepareWorkingSet and ComputeCryptoHash), obtain a new in-memory state is costful
	//and we should not clean it recklessly. instead, the state obtained from
	//db (i.e. a "commited" state) can be cached
	buildingState struct {
		blockN    uint64 //notice this is the corresponding block NUMBER, not height (blkNum+1)
		statehash []byte
		delta     *statemgmt.StateDelta
		syncing   statemgmt.DividableSyncState
	}

	cache struct {
		//the height of last state impl is used for compute
		//crypto (and can export data)
		lastWorkSet          uint64
		curHeight, refHeight uint64
		currentHash, refHash []byte
		//all the deltas, from refHeight+1 to current
		deltas []*statemgmt.StateDelta
	}

	//TODO: more cached delta for state read
}

func newStateWrapper(indexer *blockchainIndexerImpl, config *ledgerConfig) (*stateWrapper, error) {

	var err error
	sconf := state.DefaultConfig()
	if config != nil {
		sconf, err = state.NewStateConfig(config.Viper)
		if err != nil {
			return nil, err
		}
	}

	ret := &stateWrapper{
		configCached: state.StateConfig{sconf},
	}

	if err = ret.reCreate(indexer); err != nil {
		return nil, err
	}

	return ret, nil
}

func getStateImplHash(impl statemgmt.HashableState) ([]byte, error) {
	statelogger.Debug("Enter - GetHash()")
	hash, err := impl.ComputeCryptoHash()
	statelogger.Debug("Exit - GetHash()")
	if err != nil {
		return nil, err
	} else if len(hash) == 0 {
		return genesisHash, nil
	}

	return hash, nil
}

//reset the db
func (s *stateWrapper) reCreate(indexer *blockchainIndexerImpl) error {
	db := indexer.OpenchainDB
	var err error
	s.State = state.CreateState(db, s.configCached.V)

	var stateSyncing statemgmt.SyncInProgress
	err = s.GetImpl().Initialize(s.configCached.ImplConfigs())
	if v, ok := err.(statemgmt.SyncInProgress); err != nil && ok {
		stateSyncing = v
	} else if err != nil {
		return err
	}

	if stateSyncing == nil {
		currentHash, err := s.GetImpl().ComputeCryptoHash()
		if err != nil {
			return err
		}
		if len(currentHash) == 0 {
			//we replace the nil state into genesisHash, it maybe occur in non-genesis block
			s.cache.currentHash = genesisHash
			sblk, err := indexer.fetchBlockNumberByStateHash(genesisHash)
			if err == nil {
				s.cache.lastWorkSet = sblk
				if err = s.prepareCache(currentHash, sblk+1); err != nil {
					return err
				}
			} //notice if err is not nil, we consider it just gensis and continue
		} else {
			sblk, err := indexer.fetchBlockNumberByStateHash(currentHash)
			s.cache.lastWorkSet = sblk
			if err != nil {
				return err
			} else if err = s.prepareCache(currentHash, sblk+1); err != nil {
				return err
			}
		}

	} else {
		s.buildingState.statehash = stateSyncing.SyncTarget()
		s.buildingState.blockN, err = indexer.fetchBlockNumberByStateHash(s.buildingState.statehash)
		if err != nil {
			return err
		}
		//simply let it panic if covert is not avaliable
		s.buildingState.syncing = s.GetImpl().(statemgmt.DividableSyncState)
		statelogger.Infof("we start from syncing state: [%X]@%d", s.buildingState.statehash, s.buildingState.blockN)
	}

	return nil

}

func (s *stateWrapper) prepareCache(shash []byte, sheight uint64) error {
	s.cache.refHeight, s.cache.refHash = sheight, shash
	s.cache.curHeight, s.cache.currentHash = sheight, shash
	//TODO: read statedeltaCF, get more deltas into cache, calc corresponding
	//states, update currentHash
	//start from s.cache.height + 1
	return nil
}

func (s *stateWrapper) Wrapper() *state.State { return s.State }

func (s *stateWrapper) isSyncing() bool { return s.buildingState.syncing != nil }

func (s *stateWrapper) getBuildingHash() []byte {
	if len(s.buildingState.statehash) == 0 {
		return s.getHash()
	}
	return s.buildingState.statehash
}

func (s *stateWrapper) getHash() []byte {
	return s.cache.currentHash
}

func (s *stateWrapper) applyDeltaToImpl(sblk uint64, delta *statemgmt.StateDelta) error {
	s.GetImpl().ClearWorkingSet(false)
	s.cache.lastWorkSet = 0

	if err := s.GetImpl().PrepareWorkingSet(delta); err != nil {
		return err
	}
	s.cache.lastWorkSet = sblk
	return nil
}

func (s *stateWrapper) prepareState(sblk uint64, delta *statemgmt.StateDelta) error {
	//detect syncing status
	if s.isSyncing() {
		return fmt.Errorf("can not put state while syncing")
	}

	if s.cache.curHeight != sblk {
		return fmt.Errorf("can not set a state height (%d) not next to current (%d)", sblk+1, s.cache.curHeight)
	}

	workDelta := delta
	offset := s.cache.curHeight - s.cache.refHeight
	//if we cached extra delta, things become harder ...
	if offset > 0 {
		workDelta := statemgmt.NewStateDelta()
		for _, delta := range s.cache.deltas[:offset] {
			workDelta.ApplyChanges(delta)
		}
		workDelta.ApplyChanges(delta)
	}

	if err := s.applyDeltaToImpl(sblk, workDelta); err != nil {
		return err
	}
	hash, err := getStateImplHash(s.GetImpl())
	if err != nil {
		return err
	}

	s.buildingState.blockN = sblk
	s.buildingState.statehash = hash
	s.buildingState.delta = delta
	statelogger.Debugf("Prepare for commit state [%X]@%d", s.buildingState.statehash, s.buildingState.blockN)
	return nil
}

func (s *stateWrapper) commitState() {

	if s.isSyncing() {
		//nothing needs to be commited
		return
	}

	//sanity check
	if s.buildingState.blockN < s.cache.refHeight {
		panic("wrong code: build state lower than current reference")
	}

	//so we can both handling state and delta updating ...
	if deltaH := s.cache.refHeight + uint64(len(s.cache.deltas)); s.buildingState.blockN >= deltaH {
		addDeltas := make([]*statemgmt.StateDelta, s.buildingState.blockN+1-deltaH)
		s.cache.deltas = append(s.cache.deltas, addDeltas...)
	}
	s.cache.deltas[s.buildingState.blockN-s.cache.refHeight] = s.buildingState.delta

	//if we just add a delta, not change these in cache
	if len(s.buildingState.statehash) > 0 {
		s.cache.currentHash = s.buildingState.statehash
		s.cache.curHeight = s.buildingState.blockN + 1
	}

}

//notice we can only persist the top of delta stack to allow concurrent read
func (s *stateWrapper) persistentState(writeBatch *db.DBWriteBatch) error {

	pblkN := s.cache.refHeight
	shash := s.buildingState.statehash
	pdelta := s.buildingState.delta
	//notice we must check the hash because buildingState is just used for
	//putting a statedelta, not state ...
	if s.cache.lastWorkSet != pblkN || len(shash) == 0 {

		if s.cache.curHeight == s.cache.refHeight {
			//nothing to persist
			statelogger.Warningf("State not change, Nothing is written")
			return nil
		}

		//we need to re-calc hash
		pdelta = s.cache.deltas[0]
		if pdelta == nil {
			return fmt.Errorf("Delta is not ready")
		}
		//notice we must have delta here
		if err := s.applyDeltaToImpl(pblkN, pdelta); err != nil {
			return err
		}
		var err error
		shash, err = getStateImplHash(s.GetImpl())
		if err != nil {
			return err
		}
	}

	s.GetImpl().AddChangesForPersistence(writeBatch)

	//we also manage state delta ...
	cf := writeBatch.GetCFs().StateDeltaCF
	statelogger.Debugf("Adding state-delta corresponding to block number[%d]", pblkN)
	writeBatch.PutCF(cf, encodeStateDeltaKey(pblkN), pdelta.Marshal())

	if pblkN == 0 {
		statelogger.Debug("state.addChangesForPersistence()...finished for block 0")
		return nil
	}

	//"add one and delete one" policy ...
	if pblkN >= s.GetMaxStateDeltaSize() {
		blockNumberToDelete := pblkN - s.GetMaxStateDeltaSize()
		statelogger.Debugf("Deleting state-delta corresponding to block number[%d]", pblkN)
		writeBatch.DeleteCF(cf, encodeStateDeltaKey(blockNumberToDelete))
	} else {
		statelogger.Debugf("Not deleting previous state-delta. Block number [%d] is smaller than historyStateDeltaSize [%d]",
			pblkN, s.GetMaxStateDeltaSize())
	}
	statelogger.Debug("state.addChangesForPersistence()...finished")
	return nil
}

func (s *stateWrapper) persistentStateDone() {

	s.GetImpl().ClearWorkingSet(true)
	s.cache.lastWorkSet = s.cache.refHeight

	//this should be fast and no error (we just call clearworkingset)
	s.cache.refHash, _ = getStateImplHash(s.GetImpl())
	s.cache.refHeight++
	s.cache.deltas = s.cache.deltas[1:]

	for _, f := range s.updatesubscriptions {
		f(s.cache.refHeight-1, s.cache.refHash)
	}
}

func (s *stateWrapper) updatePersistentStateTo(height uint64) {
	refOffset := s.cache.curHeight - s.cache.refHeight
	s.cache.curHeight = height
	s.cache.refHeight = s.cache.curHeight - refOffset
	statelogger.Warningf("we force current state height change into %d (ref %d)", s.cache.curHeight, s.cache.refHeight)
}

func (s *stateWrapper) startSyncState(sblkn uint64, stateHash []byte) error {
	if s.isSyncing() {
		return fmt.Errorf("duplicated syncing")
	}
	convered := false
	s.buildingState.syncing, convered = s.GetImpl().(statemgmt.HashAndDividableState)
	if !convered {
		return fmt.Errorf("No syncing interface for impl [%T]", s.GetImpl())
	}

	if err := s.DeleteState(); err != nil {
		return err
	}

	statelogger.Warningf("----- Now we start a STATE SYNCING target for [%x] ----", stateHash)
	s.buildingState.blockN = sblkn
	s.buildingState.statehash = stateHash
	s.buildingState.delta = nil
	s.buildingState.syncing.InitPartialSync(stateHash)

	return nil
}

func (s *stateWrapper) getSyncing() statemgmt.DividableSyncState { return s.buildingState.syncing }

func (s *stateWrapper) preapreForSyncData(data *protos.SyncStateChunk) error {
	statelogger.Debug("----------------------------ApplyPartialSync begin -------------------------------------")
	defer statelogger.Debug("---------------------------------ApplyPartialSync end --------------------------------\n\n\n")

	if s.buildingState.delta != nil {
		s.buildingState.syncing.ClearWorkingSet(false)
		statelogger.Warning("sync another data block before previous is persisted")
	}

	s.buildingState.delta = statemgmt.NewStateDelta()
	s.buildingState.delta.ChaincodeStateDeltas = data.ChaincodeStateDeltas
	if err := s.buildingState.syncing.PrepareWorkingSet(s.buildingState.delta); err != nil {
		return err
	}

	if err := s.buildingState.syncing.ApplyPartialSync(data); err != nil {
		return err
	}

	return nil
}

func (s *stateWrapper) persistentSync(writeBatch *db.DBWriteBatch) error {
	s.buildingState.syncing.AddChangesForPersistence(writeBatch)
	return nil
}

func (s *stateWrapper) persistentSyncDone() {
	s.buildingState.delta = nil
	s.buildingState.syncing.ClearWorkingSet(true)

	if s.buildingState.syncing.IsCompleted() {
		s.cache.lastWorkSet = s.buildingState.blockN
		s.cache.refHash = s.buildingState.statehash
		s.cache.refHeight = s.buildingState.blockN + 1
		s.cache.curHeight = s.cache.refHeight
		s.cache.currentHash = s.cache.refHash

		for _, f := range s.updatesubscriptions {
			f(s.cache.refHeight-1, s.cache.refHash)
		}
	}
}

func encodeStateDeltaKey(blockNumber uint64) []byte {
	return util.EncodeUint64(blockNumber)
}
