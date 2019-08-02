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

package protos

import (
	"sort"
)

func NewChaincodeStateDelta() *ChaincodeStateDelta {
	return &ChaincodeStateDelta{UpdatedKVs: make(map[string]*UpdatedValue)}
}

func (chaincodeStateDelta *ChaincodeStateDelta) Get(key string) *UpdatedValue {
	return chaincodeStateDelta.UpdatedKVs[key]
}

func (chaincodeStateDelta *ChaincodeStateDelta) Set(key string, updatedValue, previousValue []byte) {

	var updatedV *UpdatedValue_VSlice
	if updatedValue != nil {
		updatedV = &UpdatedValue_VSlice{Value: updatedValue}
	}

	updatedKV, ok := chaincodeStateDelta.UpdatedKVs[key]
	if ok {
		// Key already exists, just set the updated value
		updatedKV.ValueWrap = updatedV
	} else {
		// New key. Create a new entry in the map
		chaincodeStateDelta.UpdatedKVs[key] = &UpdatedValue{
			ValueWrap:     updatedV,
			PreviousValue: previousValue}
	}
}

func (chaincodeStateDelta *ChaincodeStateDelta) Remove(key string, previousValue []byte) {
	updatedKV, ok := chaincodeStateDelta.UpdatedKVs[key]
	if ok {
		// Key already exists, just set the value
		updatedKV.ValueWrap = nil
	} else {
		// New key. Create a new entry in the map
		chaincodeStateDelta.UpdatedKVs[key] = &UpdatedValue{PreviousValue: previousValue}
	}
}

func (chaincodeStateDelta *ChaincodeStateDelta) HasChanges() bool {
	return len(chaincodeStateDelta.UpdatedKVs) > 0
}

func (chaincodeStateDelta *ChaincodeStateDelta) GetSortedKeys() []string {
	updatedKeys := []string{}
	for k := range chaincodeStateDelta.UpdatedKVs {
		updatedKeys = append(updatedKeys, k)
	}
	sort.Strings(updatedKeys)
	logger.Debugf("Sorted keys = %#v", updatedKeys)
	return updatedKeys
}

// IsDeleted checks whether the key was deleted
func (updatedValue *UpdatedValue) IsDeleted() bool {
	return updatedValue.GetValue() == nil
}

func (m *UpdatedValue) GetValue() []byte {
	if m != nil {
		if m.ValueWrap == nil {
			return nil
		} else if m.ValueWrap.Value == nil {
			return []byte{}
		} else {
			return m.ValueWrap.Value
		}

	}
	return nil
}
