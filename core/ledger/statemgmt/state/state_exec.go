package state

import (
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger/statemgmt"
)

// supporting for the concurrent executation of tx
// the r/w in tx exec involve three sets:
// wset record all set/delete action
// rset trace all get action, so we can evaluate the whole cost to db for one
// tx, we also trace each key in the iteration op in tx.
// baseSets, which is an array, is also used for get operation
type TxExecStates struct {
	wset     *statemgmt.StateDelta
	rset     *statemgmt.StateDelta
	baseSets []*statemgmt.StateDelta
	sn       *db.DBSnapshot
}

func NewExecStates(baseSets ...*statemgmt.StateDelta) *TxExecStates {

	//filter nil arguments
	var filteredEnd int
	for _, set := range baseSets {
		if set != nil {
			baseSets[filteredEnd] = set
			filteredEnd++
		}
	}

	return &TxExecStates{
		wset:     statemgmt.NewStateDelta(),
		rset:     statemgmt.NewStateDelta(),
		baseSets: baseSets[:filteredEnd],
	}
}

//legacy API
func NewExecStatesFromState(s *State) *TxExecStates {
	return NewExecStates(s.stateDelta)
}

//caller MUST response for releasing the snapshot
func (s *TxExecStates) BindSnapshot(sn *db.DBSnapshot) { s.sn = sn }

func (s *TxExecStates) IsSnapshotRead() bool { return s.sn != nil }

func (s *TxExecStates) DeRef() *statemgmt.StateDelta {
	return s.wset
}

//overwrite statedelta's isempty
func (s *TxExecStates) IsEmpty() bool {
	return s.wset.IsEmpty()
}

func (s *TxExecStates) TxExecResultHash() []byte {
	return s.wset.ComputeCryptoHash()
}

func (s *TxExecStates) get(chaincodeID string, key string, underlying statemgmt.HashableState) ([]byte, error) {
	if s.sn == nil {
		return underlying.Get(chaincodeID, key)
	} else {
		return underlying.GetSafe(s.sn, chaincodeID, key)
	}
}

func (s *TxExecStates) Get(chaincodeID string, key string, underlying statemgmt.HashableState) (ret []byte, err error) {

	if valueHolder := s.wset.Get(chaincodeID, key); valueHolder != nil {
		return valueHolder.GetValue(), nil
	}

	//try to fetch from cache first
	if valueHolder := s.rset.Get(chaincodeID, key); valueHolder != nil {
		return valueHolder.GetValue(), nil
	}

	//notice we trace the read result even it is not exist, later rset can get valueHolder
	//and return nil
	defer func() {
		if err == nil {
			s.rset.Set(chaincodeID, key, ret, ret)
		}
	}()

	for _, set := range s.baseSets {
		if valueHolder := set.Get(chaincodeID, key); valueHolder != nil {
			return valueHolder.GetValue(), nil
		}
	}

	return underlying.Get(chaincodeID, key)
}

// we can skip "original" repair in wset, which build a forward only delta, instead
// of the original bi-direction fashion. in-memory cache and checkpoint can help
// us rollback or query states, instead of apply a "rollback" delta. and we can get more
// efficient in some blockchain implement like PBFT

// TODO: cost of repairwset is not count yet
func (s *TxExecStates) RepairWSet(underlying statemgmt.HashableState) error {
	for cc, cckv := range s.wset.ChaincodeStateDeltas {
		rcckv := s.rset.GetUpdates(cc)
		if rcckv == nil {
			rcckv = statemgmt.UpdatesNothing
		}
		for k, v := range cckv.GetUpdatedKVs() {
			if v.GetPreviousValue() == nil {
				if _, hasread := rcckv[k]; !hasread {
					prevV, err := underlying.Get(cc, k)
					if err != nil {
						return err
					}
					v.PreviousValue = prevV
				}
			}
		}
	}
	return nil
}

// we overwrite the set/delete methods with the fashion in state module:
// previous value is only searched from in-memory variables we availiable (i.e: rset/baseSets)
// base on the purpose that the most common op is write-after-read
// we can also repair the wset's previous value later if needed
func (s *TxExecStates) Set(chaincodeID string, key string, value []byte) {
	//we simply lookup previous value from rset, so no error will occur
	s.wset.Set(chaincodeID, key, value, s.rset.Get(chaincodeID, key).GetValue())
}

func (s *TxExecStates) Delete(chaincodeID string, key string) {
	s.wset.Delete(chaincodeID, key, s.rset.Get(chaincodeID, key).GetValue())
}

// SetMultipleKeys sets the values for the multiple keys.
func (s *TxExecStates) SetMultipleKeys(chaincodeID string, kvs map[string][]byte) {
	refU := s.rset.GetUpdates(chaincodeID)
	targetU := s.wset.SafeGetUpdates(chaincodeID)
	for k, v := range kvs {
		var refV []byte
		if refU != nil {
			refV = refU[k].GetValue()
		}
		targetU.Set(k, v, refV)
	}
}

// GetRangeScanIterator returns an iterator to get all the keys (and values) between startKey and endKey
// (assuming lexical order of the keys) for a chaincodeID.
func (s *TxExecStates) GetRangeScanIterator(chaincodeID string, startKey string, endKey string, underlying statemgmt.HashableState) (statemgmt.RangeScanIterator, error) {

	var stateImplItr statemgmt.RangeScanIterator
	var err error

	stateImplItr, err = underlying.GetRangeScanIterator(chaincodeID, startKey, endKey)
	if err != nil {
		return nil, err
	}

	defer stateImplItr.Close()
	//TODO: should cache all iterating range to avoid unecessary access of db?
	implItr := statemgmt.NewStateDeltaRangeScanIteratorFromRaw(s.rset,
		stateImplItr, chaincodeID, startKey, endKey)

	deltas := []*statemgmt.StateDeltaIterator{statemgmt.NewStateDeltaRangeScanIterator(s.wset, chaincodeID, startKey, endKey)}
	for _, set := range s.baseSets {
		deltas = append(deltas, statemgmt.NewStateDeltaRangeScanIterator(set,
			chaincodeID, startKey, endKey))
	}
	deltas = append(deltas, implItr)

	return newCompositeRangeScanIterator(deltas...), nil
}
