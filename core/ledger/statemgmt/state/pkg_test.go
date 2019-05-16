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
	"os"
	"testing"

	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/core/ledger/testutil"
)

var testDBWrapper = db.NewTestDBWrapper()

func TestMain(m *testing.M) {
	testutil.SetupTestConfig()
	os.Exit(m.Run())
}

func createFreshDBAndConstructState(t *testing.T) (*stateTestWrapper, *State) {
	testDBWrapper.CleanDB(t)
	stateTestWrapper := newStateTestWrapper(t)
	return stateTestWrapper, stateTestWrapper.state
}

type stateTestWrapper struct {
	t     *testing.T
	state *State
}

func newStateTestWrapper(t *testing.T) *stateTestWrapper {
	return &stateTestWrapper{t, NewState(testDBWrapper.GetDB(), DefaultConfig())}
}

func (testWrapper *stateTestWrapper) get(chaincodeID string, key string, committed bool) []byte {
	var value []byte
	var err error
	if committed {
		value, err = testWrapper.state.stateImpl.Get(chaincodeID, key)
	} else {
		value, err = testWrapper.state.GetTransient(chaincodeID, key)
	}
	testutil.AssertNoError(testWrapper.t, err, "Error while getting state")
	return value
}

func (testWrapper *stateTestWrapper) getSnapshot() *StateSnapshot {
	dbSnapshot := db.GetDBHandle().GetSnapshot()
	stateSnapshot, err := testWrapper.state.GetSnapshot(0, dbSnapshot)
	testutil.AssertNoError(testWrapper.t, err, "Error during creation of state snapshot")
	return stateSnapshot
}

func (testWrapper *stateTestWrapper) persistAndClearInMemoryChanges(blockNumber uint64) {
	writeBatch := testDBWrapper.NewWriteBatch()
	defer writeBatch.Destroy()
	testWrapper.state.AddChangesForPersistence(blockNumber, writeBatch)
	testDBWrapper.WriteToDB(testWrapper.t, writeBatch)
	testWrapper.state.ClearInMemoryChanges(true)
}

func (testWrapper *stateTestWrapper) fetchStateDeltaFromDB(blockNumber uint64) *statemgmt.StateDelta {
	delta := statemgmt.NewStateDelta()
	delta.Unmarshal(testDBWrapper.GetFromStateDeltaCF(testWrapper.t, encodeStateDeltaKey(blockNumber)))
	return delta
}
