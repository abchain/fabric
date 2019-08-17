/*
Copyright IBM Corp. 2016 All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

		 http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package state

import (
	"fmt"

	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/core/ledger/statemgmt/buckettree"
	"github.com/abchain/fabric/core/ledger/statemgmt/raw"
	"github.com/abchain/fabric/core/ledger/statemgmt/trie"
	"github.com/abchain/fabric/core/util"
	"github.com/op/go-logging"
)

var logger = logging.MustGetLogger("state")

const defaultStateImpl = "buckettree"

type stateImplType string

const (
	buckettreeType stateImplType = "buckettree"
	trieType       stateImplType = "trie"
	rawType        stateImplType = "raw"
)

// State structure for maintaining world state.
// This encapsulates a particular implementation for managing the state persistence
// This is not thread safe
type State struct {
	//this three fields MUST be immutable in the life-time of a ledger
	//(even the db has been switched)
	*db.OpenchainDB

	stateImpl  statemgmt.HashableState
	stateDelta *statemgmt.StateDelta

	updateStateImpl       bool
	historyStateDeltaSize uint64

	//notice all of the tx-related API and variables has been DEPRECATED
	currentTxID      string
	txStateDeltaHash map[string][]byte
	txExec           *TxExecStates
}

// CreateState constructs a new State, but not initialize it
func CreateState(db *db.OpenchainDB, config *stateConfig) *State {

	var stateImpl statemgmt.HashableState
	logger.Infof("Initializing state implementation [%s]", config.stateImplName)
	switch config.stateImplName {
	case buckettreeType:
		stateImpl = buckettree.NewStateImpl(db)
	case trieType:
		stateImpl = trie.NewStateImpl(db)
	case rawType:
		stateImpl = raw.NewStateImpl(db)
	default:
		panic("Should not reach here. Configs should have checked for the stateImplName being a valid names ")
	}

	return &State{db, stateImpl, statemgmt.NewStateDelta(),
		false, uint64(config.deltaHistorySize), "", make(map[string][]byte), nil}
}

// NewState constructs a new State. This Initializes encapsulated state implementation
func NewState(db *db.OpenchainDB, config *stateConfig) *State {

	var stateImpl statemgmt.HashableState
	logger.Infof("Initializing state implementation [%s]", config.stateImplName)
	switch config.stateImplName {
	case buckettreeType:
		stateImpl = buckettree.NewStateImpl(db)
	case trieType:
		stateImpl = trie.NewStateImpl(db)
	case rawType:
		stateImpl = raw.NewStateImpl(db)
	default:
		panic("Should not reach here. Configs should have checked for the stateImplName being a valid names ")
	}
	err := stateImpl.Initialize(config.stateImplConfigs)
	if err != nil {
		panic(fmt.Errorf("Error during initialization of state implementation: %s", err))
	}

	return &State{db, stateImpl, statemgmt.NewStateDelta(),
		false, uint64(config.deltaHistorySize), "", make(map[string][]byte), nil}
}

// getStateDelta get changes in state after most recent call to method clearInMemoryChanges
func (state *State) getStateDelta() *statemgmt.StateDelta {
	return state.stateDelta
}

func (state *State) GetMaxStateDeltaSize() uint64 { return state.historyStateDeltaSize }

func (state *State) GetSnapshotManager() statemgmt.SnapshotState {
	switch impl := state.stateImpl.(type) {
	case *buckettree.StateImpl:
		return impl.NewSnapshotState()
	default:
		return &snapshotMgrAdapter{bindedImpl: state.stateImpl}
	}
}

// GetSnapshot returns a snapshot of the global state for the current block. stateSnapshot.Release()
// must be called once you are done.
func (state *State) GetSnapshot(blockNumber uint64, dbSnapshot *db.DBSnapshot) (*StateSnapshot, error) {
	return newStateSnapshot(state, blockNumber, dbSnapshot)
}

// FetchStateDeltaFromDB fetches the StateDelta corrsponding to given blockNumber
func (state *State) FetchStateDeltaFromDB(blockNumber uint64) (*statemgmt.StateDelta, error) {
	stateDeltaBytes, err := state.GetValue(db.StateDeltaCF, encodeStateDeltaKey(blockNumber))
	if err != nil {
		return nil, err
	}
	if stateDeltaBytes == nil {
		return nil, nil
	}
	stateDelta := statemgmt.NewStateDelta()
	stateDelta.Unmarshal(stateDeltaBytes)
	return stateDelta, nil
}

//func (state *State) AddChangesForPersistence(blockNumber uint64, writeBatch *db.DBWriteBatch) {
//	state.stateImpl.PrepareWorkingSet(state.stateDelta)
//	state.stateImpl.AddChangesForPersistence(writeBatch)
//}

// AddChangesForPersistence adds key-value pairs to writeBatch
func (state *State) AddChangesForPersistence(blockNumber uint64, writeBatch *db.DBWriteBatch) {
	logger.Debug("state.addChangesForPersistence()...start")
	if state.updateStateImpl {
		state.stateImpl.PrepareWorkingSet(state.stateDelta)
		state.updateStateImpl = false
	}
	state.stateImpl.AddChangesForPersistence(writeBatch)

	serializedStateDelta := state.stateDelta.Marshal()
	cf := writeBatch.GetCFs().StateDeltaCF
	logger.Debugf("Adding state-delta corresponding to block number[%d]", blockNumber)
	writeBatch.PutCF(cf, encodeStateDeltaKey(blockNumber), serializedStateDelta)

	if blockNumber == 0 {
		logger.Debug("state.addChangesForPersistence()...finished for block 0")
		return
	}

	if blockNumber >= state.historyStateDeltaSize {
		blockNumberToDelete := blockNumber - state.historyStateDeltaSize
		logger.Debugf("Deleting state-delta corresponding to block number[%d]", blockNumberToDelete)
		writeBatch.DeleteCF(cf, encodeStateDeltaKey(blockNumberToDelete))
	} else {
		logger.Debugf("Not deleting previous state-delta. Block number [%d] is smaller than historyStateDeltaSize [%d]",
			blockNumber, state.historyStateDeltaSize)
	}
	logger.Debug("state.addChangesForPersistence()...finished")
}

// ----- Deprecated Bellow ------

var genesisHash = []byte("YAFABRIC09_GENSISHASH")

// GetHash computes new state hash if the stateDelta is to be applied.
// Recomputes only if stateDelta has changed after most recent call to this function
func (state *State) GetHash() ([]byte, error) {
	logger.Debug("Enter - GetHash()")
	if state.updateStateImpl {
		logger.Debug("updating stateImpl with working-set")
		state.stateImpl.PrepareWorkingSet(state.stateDelta)
		state.updateStateImpl = false
	}
	hash, err := state.stateImpl.ComputeCryptoHash()
	if err != nil {
		return nil, err
	}
	logger.Debug("Exit - GetHash()")
	//we avoid nil-hash and change it
	if hash == nil {
		hash = genesisHash
	}
	return hash, nil
}

// ClearInMemoryChanges remove from memory all the changes to state
func (state *State) ClearInMemoryChanges(changesPersisted bool) {
	state.stateDelta = statemgmt.NewStateDelta()
	state.txStateDeltaHash = make(map[string][]byte)
	state.stateImpl.ClearWorkingSet(changesPersisted)
}

// ApplyStateDelta applies already prepared stateDelta to the existing state.
// This is an in memory change only. state.CommitStateDelta must be used to
// commit the state to the DB. This method is to be used in state transfer.
func (state *State) ApplyStateDelta(delta *statemgmt.StateDelta) {
	state.stateDelta = delta
	state.updateStateImpl = true
}

//similar to ApplyStateDelta but it merge, instead of change the state
func (state *State) MergeStateDelta(delta *statemgmt.StateDelta) {
	state.stateDelta.ApplyChanges(delta)
	state.updateStateImpl = true
}

// CommitStateDelta commits the changes from state.ApplyStateDelta to the
// DB.
func (state *State) CommitStateDelta() error {

	if state.updateStateImpl {
		state.stateImpl.PrepareWorkingSet(state.stateDelta)
		state.updateStateImpl = false
	}
	writeBatch := state.NewWriteBatch()
	defer writeBatch.Destroy()
	state.stateImpl.AddChangesForPersistence(writeBatch)
	return writeBatch.BatchCommit()
}

// ----- Deprecated Above ------

// DeleteState deletes ALL state keys/values from the DB. This is generally
// only used during state synchronization when creating a new state from
// a snapshot.
func (state *State) DeleteState() error {

	state.ClearInMemoryChanges(false)
	err := state.OpenchainDB.DeleteState()
	if err != nil {
		logger.Errorf("Error deleting state: %s", err)
	}
	return err

}

func (state *State) GetStateDelta() *statemgmt.StateDelta {
	return state.getStateDelta()
}

func (state *State) GetImpl() statemgmt.HashableState {
	return state.stateImpl
}

// func (state *State) GetDividableState() statemgmt.HashAndDividableState {
// 	inf, ok := state.stateImpl.(statemgmt.HashAndDividableState)
// 	if ok {
// 		return inf
// 	} else {
// 		return nil
// 	}
// }

func encodeStateDeltaKey(blockNumber uint64) []byte {
	return util.EncodeUint64(blockNumber)
}

func decodeStateDeltaKey(dbkey []byte) uint64 {
	return util.DecodeToUint64(dbkey)
}
