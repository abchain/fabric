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
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"sync"
)

// StateSnapshot encapsulates StateSnapshotIterator given by actual state implementation and the db snapshot
type StateSnapshot struct {
	blockNumber  uint64
	stateImplItr statemgmt.StateSnapshotIterator
	dbSnapshot   *db.DBSnapshot
}

func NewStateSnapshot(blockNumber uint64, itr statemgmt.StateSnapshotIterator, dbSnapshot *db.DBSnapshot) *StateSnapshot {
	return &StateSnapshot{blockNumber, itr, dbSnapshot}
}

// newStateSnapshot creates a new snapshot of the global state for the current block.
func newStateSnapshot(s *State, blockNumber uint64, dbSnapshot *db.DBSnapshot) (*StateSnapshot, error) {
	itr, err := s.stateImpl.GetStateSnapshotIterator(dbSnapshot)
	if err != nil {
		return nil, err
	}
	snapshot := &StateSnapshot{blockNumber, itr, dbSnapshot}
	return snapshot, nil
}

// Release the snapshot. This MUST be called when you are done with this resouce.
func (ss *StateSnapshot) Release() {
	ss.stateImplItr.Close()
	ss.dbSnapshot.Release()
}

// Next moves the iterator to the next key/value pair in the state
func (ss *StateSnapshot) Next() bool {
	return ss.stateImplItr.Next()
}

// GetRawKeyValue returns the raw bytes for the key and value at the current iterator position
func (ss *StateSnapshot) GetRawKeyValue() ([]byte, []byte) {
	return ss.stateImplItr.GetRawKeyValue()
}

// GetBlockNumber returns the blocknumber associated with this global state snapshot
func (ss *StateSnapshot) GetBlockNumber() uint64 {
	return ss.blockNumber
}

//bind an state object to provide an "as is" implement for the SnapshotState interface
type snapshotMgrAdapter struct {
	sync.Mutex
	bindedImpl statemgmt.HashableState
}

func (sa *snapshotMgrAdapter) GetStateSnapshotIterator(sn *db.DBSnapshot) (statemgmt.StateSnapshotIterator, error) {
	sa.Lock()
	defer sa.Unlock()
	return sa.bindedImpl.GetStateSnapshotIterator(sn)
}

func (sa *snapshotMgrAdapter) GetPartialRangeIterator(sn *db.DBSnapshot) (statemgmt.PartialRangeIterator, error) {
	sa.Lock()
	defer sa.Unlock()

	inf, ok := sa.bindedImpl.(statemgmt.HashAndDividableState)
	if ok {
		return inf.GetPartialRangeIterator(sn)
	} else {
		return nil, nil
	}
}

func (sa *snapshotMgrAdapter) BindImpl(impl statemgmt.HashableState) {
	sa.Lock()
	defer sa.Unlock()

	sa.bindedImpl = impl
}
