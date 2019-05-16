package state

import (
	"fmt"
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger/statemgmt"
)

// the tx-related API which has been deprecated (use TxExecStates instead)
// GetTxStateDeltaHash return the hash of the StateDelta
func (state *State) GetTxStateDeltaHash() map[string][]byte {
	return state.txStateDeltaHash
}

// TxBegin marks begin of a new tx. If a tx is already in progress, this call panics
func (state *State) TxBegin(txID string) {
	logger.Debugf("txBegin() for txId [%s]", txID)
	if state.txInProgress() {
		panic(fmt.Errorf("A tx [%s] is already in progress. Received call for begin of another tx [%s]", state.currentTxID, txID))
	}
	state.currentTxID = txID
	if state.stateDelta == nil {
		panic("stateDelta is not ready")
	}
	state.txExec = NewExecStatesFromState(state)
}

func (state *State) ApplyTxExec(exec *TxExecStates) {
	state.txExec = exec
}

// TxFinish marks the completion of on-going tx. If txID is not same as of the on-going tx, this call panics
func (state *State) TxFinish(txID string, txSuccessful bool) {
	logger.Debugf("txFinish() for txId [%s], txSuccessful=[%t]", txID, txSuccessful)
	if state.currentTxID != txID {
		panic(fmt.Errorf("Different txId in tx-begin [%s] and tx-finish [%s]", state.currentTxID, txID))
	}
	if txSuccessful {
		if !state.txExec.IsEmpty() {
			logger.Debugf("txFinish() for txId [%s] merging state changes", txID)
			state.txExec.RepairWSet(state.stateImpl)
			state.stateDelta.ApplyChanges(state.txExec.wset)
			state.txStateDeltaHash[txID] = state.txExec.TxExecResultHash()
			state.updateStateImpl = true
		} else {
			state.txStateDeltaHash[txID] = nil
		}
	}
	state.txExec = nil
	state.currentTxID = ""
}

func (state *State) txInProgress() bool {
	return state.currentTxID != ""
}

// Get returns state for chaincodeID and key. If committed is false, this first looks in memory and if missing,
// pulls from db. If committed is true, this pulls from the db only.
func (state *State) Get(chaincodeID string, key string, sn *db.DBSnapshot, _ int) ([]byte, error) {

	if sn != nil {
		return state.stateImpl.GetSafe(sn, chaincodeID, key)
	} else {
		return state.stateImpl.Get(chaincodeID, key)
	}

}

func (state *State) GetTransient(chaincodeID string, key string) ([]byte, error) {

	if !state.txInProgress() {
		tmpExec := NewExecStatesFromState(state)
		return tmpExec.Get(chaincodeID, key, state.stateImpl)
	}

	return state.txExec.Get(chaincodeID, key, state.stateImpl)
}

// GetRangeScanIterator returns an iterator to get all the keys (and values) between startKey and endKey
// (assuming lexical order of the keys) for a chaincodeID.
func (state *State) GetRangeScanIterator(chaincodeID string, startKey string, endKey string, committed bool) (statemgmt.RangeScanIterator, error) {

	if !committed {
		if !state.txInProgress() {
			panic("State can be changed only in context of a tx.")
		}
		return state.txExec.GetRangeScanIterator(chaincodeID, startKey, endKey, state.stateImpl)
	}

	stateImplItr, err := state.stateImpl.GetRangeScanIterator(chaincodeID, startKey, endKey)
	if err != nil {
		return nil, err
	}
	defer stateImplItr.Close()
	implItr := statemgmt.NewStateDeltaRangeScanIteratorFromRaw(statemgmt.NewStateDelta(),
		stateImplItr, chaincodeID, startKey, endKey)

	return implItr, nil
}

// Set sets state to given value for chaincodeID and key. Does not immediately writes to DB
func (state *State) Set(chaincodeID string, key string, value []byte) error {
	logger.Debugf("set() chaincodeID=[%s], key=[%s], value=[%#v]", chaincodeID, key, value)
	if !state.txInProgress() {
		panic("State can be changed only in context of a tx.")
	}

	state.txExec.Set(chaincodeID, key, value)
	return nil
}

// Delete tracks the deletion of state for chaincodeID and key. Does not immediately writes to DB
func (state *State) Delete(chaincodeID string, key string) error {
	logger.Debugf("delete() chaincodeID=[%s], key=[%s]", chaincodeID, key)
	if !state.txInProgress() {
		panic("State can be changed only in context of a tx.")
	}

	state.txExec.Delete(chaincodeID, key)
	return nil
}

// CopyState copies all the key-values from sourceChaincodeID to destChaincodeID
func (state *State) CopyState(sourceChaincodeID string, destChaincodeID string) error {

	if !state.txInProgress() {
		panic("State can be changed only in context of a tx.")
	}

	itr, err := state.GetRangeScanIterator(sourceChaincodeID, "", "", true)
	defer itr.Close()
	if err != nil {
		return err
	}
	for itr.Next() {
		k, v := itr.GetKeyValue()
		state.txExec.Set(destChaincodeID, k, v)
	}
	return nil
}

// // GetMultipleKeys returns the values for the multiple keys.
// func (state *State) GetMultipleKeys(chaincodeID string, keys []string, committed bool) ([][]byte, error) {
// 	var values [][]byte
// 	for _, k := range keys {
// 		v, err := state.Get(chaincodeID, k, committed)
// 		if err != nil {
// 			return nil, err
// 		}
// 		values = append(values, v)
// 	}
// 	return values, nil
// }

// SetMultipleKeys sets the values for the multiple keys.
func (state *State) SetMultipleKeys(chaincodeID string, kvs map[string][]byte) error {
	for k, v := range kvs {
		err := state.Set(chaincodeID, k, v)
		if err != nil {
			return err
		}
	}
	return nil
}
