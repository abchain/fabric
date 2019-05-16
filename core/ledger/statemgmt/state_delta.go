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

package statemgmt

import (
	"bytes"
	"sort"

	"github.com/abchain/fabric/core/util"
	pb "github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"

	"fmt"
)

// StateDelta holds the changes to existing state. This struct is used for holding the uncommitted changes during execution of a tx-batch
// Also, to be used for transferring the state to another peer in chunks
type StateDelta struct {
	ChaincodeStateDeltas map[string]*pb.ChaincodeStateDelta
	// RollBackwards allows one to contol whether this delta will roll the state
	// forwards or backwards.
	RollBackwards bool
}

// NewStateDelta constructs an empty StateDelta struct
func NewStateDelta() *StateDelta {
	return &StateDelta{make(map[string]*pb.ChaincodeStateDelta), false}
}

func GenUpdateStateDelta(delta map[string]*pb.ChaincodeStateDelta) *StateDelta {
	return &StateDelta{delta, false}
}

// Get get the state from delta if exists
func (stateDelta *StateDelta) Get(chaincodeID string, key string) *pb.UpdatedValue {
	if stateDelta == nil {
		return nil
	}
	// TODO Cache?
	chaincodeStateDelta, ok := stateDelta.ChaincodeStateDeltas[chaincodeID]
	if ok {
		return chaincodeStateDelta.Get(key)
	}
	return nil
}

// Set sets state value for a key
func (stateDelta *StateDelta) Set(chaincodeID string, key string, value, previousValue []byte) {
	chaincodeStateDelta := stateDelta.getOrCreateChaincodeStateDelta(chaincodeID)
	chaincodeStateDelta.Set(key, value, previousValue)
	return
}

// Delete deletes a key from the state
func (stateDelta *StateDelta) Delete(chaincodeID string, key string, previousValue []byte) {
	chaincodeStateDelta := stateDelta.getOrCreateChaincodeStateDelta(chaincodeID)
	chaincodeStateDelta.Remove(key, previousValue)
	return
}

// IsUpdatedValueSet returns true if a update value is already set for
// the given chaincode ID and key.
func (stateDelta *StateDelta) IsUpdatedValueSet(chaincodeID, key string) bool {
	chaincodeStateDelta, ok := stateDelta.ChaincodeStateDeltas[chaincodeID]
	if !ok {
		return false
	}
	if _, ok := chaincodeStateDelta.UpdatedKVs[key]; ok {
		return true
	}
	return false
}

// ApplyChanges merges another delta - if a key is present in both, the value of the existing key is overwritten
func (stateDelta *StateDelta) ApplyChanges(anotherStateDelta *StateDelta) {
	for chaincodeID, chaincodeStateDelta := range anotherStateDelta.ChaincodeStateDeltas {
		existingChaincodeStateDelta, existingChaincode := stateDelta.ChaincodeStateDeltas[chaincodeID]
		for key, valueHolder := range chaincodeStateDelta.UpdatedKVs {
			var previousValue []byte
			if existingChaincode {
				existingUpdateValue, existingUpdate := existingChaincodeStateDelta.UpdatedKVs[key]
				if existingUpdate {
					// The existing state delta already has an updated value for this key.
					previousValue = existingUpdateValue.PreviousValue
				} else {
					// Use the previous value set in the new state delta
					previousValue = valueHolder.PreviousValue
				}
			} else {
				// Use the previous value set in the new state delta
				previousValue = valueHolder.PreviousValue
			}

			if valueHolder.IsDeleted() {
				stateDelta.Delete(chaincodeID, key, previousValue)
			} else {
				stateDelta.Set(chaincodeID, key, valueHolder.GetValue(), previousValue)
			}
		}
	}
}

// IsEmpty checks whether StateDelta contains any data
func (stateDelta *StateDelta) IsEmpty() bool {
	return len(stateDelta.ChaincodeStateDeltas) == 0
}

// GetUpdatedChaincodeIds return the chaincodeIDs that are prepsent in the delta
// If sorted is true, the method return chaincodeIDs in lexicographical sorted order
func (stateDelta *StateDelta) GetUpdatedChaincodeIds(sorted bool) []string {
	updatedChaincodeIds := make([]string, len(stateDelta.ChaincodeStateDeltas))
	i := 0
	for k := range stateDelta.ChaincodeStateDeltas {
		updatedChaincodeIds[i] = k
		i++
	}
	if sorted {
		sort.Strings(updatedChaincodeIds)
	}
	return updatedChaincodeIds
}

//maybe useful for some case
var UpdatesNothing = map[string]*pb.UpdatedValue{}

// GetUpdates returns changes associated with given chaincodeId
func (stateDelta *StateDelta) GetUpdates(chaincodeID string) map[string]*pb.UpdatedValue {
	chaincodeStateDelta := stateDelta.ChaincodeStateDeltas[chaincodeID]
	if chaincodeStateDelta == nil {
		return nil
	}
	return chaincodeStateDelta.UpdatedKVs
}

func (stateDelta *StateDelta) SafeGetUpdates(chaincodeID string) *pb.ChaincodeStateDelta {
	return stateDelta.getOrCreateChaincodeStateDelta(chaincodeID)
}

func (stateDelta *StateDelta) getOrCreateChaincodeStateDelta(chaincodeID string) *pb.ChaincodeStateDelta {
	chaincodeStateDelta, ok := stateDelta.ChaincodeStateDeltas[chaincodeID]
	if !ok {
		chaincodeStateDelta = pb.NewChaincodeStateDelta()
		stateDelta.ChaincodeStateDeltas[chaincodeID] = chaincodeStateDelta
	}
	return chaincodeStateDelta
}

// ComputeCryptoHash computes crypto-hash for the data held
// returns nil if no data is present
func (stateDelta *StateDelta) ComputeCryptoHash() []byte {
	if stateDelta.IsEmpty() {
		return nil
	}
	var buffer bytes.Buffer
	sortedChaincodeIds := stateDelta.GetUpdatedChaincodeIds(true)
	for _, chaincodeID := range sortedChaincodeIds {
		buffer.WriteString(chaincodeID)
		chaincodeStateDelta := stateDelta.ChaincodeStateDeltas[chaincodeID]
		sortedKeys := chaincodeStateDelta.GetSortedKeys()
		for _, key := range sortedKeys {
			buffer.WriteString(key)
			updatedValue := chaincodeStateDelta.Get(key)
			if !updatedValue.IsDeleted() {
				buffer.Write(updatedValue.GetValue())
			}
		}
	}
	hashingContent := buffer.Bytes()
	logger.Debugf("computing hash on %#v", hashingContent)
	return util.ComputeCryptoHash(hashingContent)
}

// marshalling / Unmarshalling code
// We need to revisit the following when we define proto messages
// for state related structures for transporting. May be we can
// completely get rid of custom marshalling / Unmarshalling of a state delta

// Marshal serializes the StateDelta
func (stateDelta *StateDelta) Marshal() []byte {

	syncStateChunk := &pb.SyncStateChunk{}

	syncStateChunk.ChaincodeStateDeltas = stateDelta.ChaincodeStateDeltas

	b, err := proto.Marshal(syncStateChunk)

	if err != nil {
		//return fmt.Errorf("Error unmarashaling size: %s", err)
		return nil
	}

	return b
}

//// Unmarshal deserializes StateDelta
func (stateDelta *StateDelta) Unmarshal(bytes []byte) error {

	syncStateChunk := &pb.SyncStateChunk{}

	err := proto.Unmarshal(bytes, syncStateChunk)

	if err != nil {
		return fmt.Errorf("Error unmarashaling size: %s", err)
	}

	stateDelta.ChaincodeStateDeltas = syncStateChunk.ChaincodeStateDeltas
	return nil
}
