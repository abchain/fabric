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
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"strings"
)

// CompositeRangeScanIterator - an implementation of interface 'statemgmt.RangeScanIterator'
// This provides a wrapper on top of more than one underlying iterators
type CompositeRangeScanIterator struct {
	itrs        []*statemgmt.StateDeltaIterator
	currentItrs []int
}

func newCompositeRangeScanIterator(itrs ...*statemgmt.StateDeltaIterator) statemgmt.RangeScanIterator {
	ret := &CompositeRangeScanIterator{itrs: itrs}
	ret.init()
	return ret
}

//fill every number for first tasks
func (citr *CompositeRangeScanIterator) init() {
	for i, _ := range citr.itrs {
		citr.currentItrs = append(citr.currentItrs, i)
	}
}

func (citr *CompositeRangeScanIterator) next() bool {

	if len(citr.currentItrs) == 0 {
		return false
	}

	logger.Debugf("Next task: currentItrNumbers = %v", citr.currentItrs)
	var lastPos int
	for i, itr := range citr.itrs {

		if len(citr.currentItrs) > 0 && i == citr.currentItrs[0] {
			citr.currentItrs = citr.currentItrs[1:]
			if !itr.NextWithDeleted() {
				logger.Debugf("iterator %d is over", i)
				continue
			}
		}
		citr.itrs[lastPos] = itr
		lastPos++
	}
	citr.itrs = citr.itrs[:lastPos]

	//scan for next availiable values
	//merge sort, from top to bottom, notice same value will be also considered ...
	var minKey string
	for i, itr := range citr.itrs {
		k, _ := itr.GetKeyValue()
		if cmp := strings.Compare(k, minKey); minKey == "" || cmp < 0 {
			citr.currentItrs = []int{i}
			minKey = k
		} else if cmp == 0 {
			citr.currentItrs = append(citr.currentItrs, i)
		}
	}
	logger.Debugf("Evaluating key = %s currentItrNumbers = %v", minKey, citr.currentItrs)
	return len(citr.currentItrs) > 0
}

func (citr *CompositeRangeScanIterator) Next() bool {
	for citr.next() {
		if k, v := citr.itrs[citr.currentItrs[0]].GetKeyRawValue(); v.IsDeleted() {
			logger.Debugf("Evaluating key = %s is deleted key, skip for next", k)
		} else {
			return true
		}
	}
	return false
}

// GetKeyValue - see interface 'statemgmt.StateDeltaIterator' for details
func (citr *CompositeRangeScanIterator) GetKeyValue() (string, []byte) {
	return citr.itrs[citr.currentItrs[0]].GetKeyValue()
}

// Close - see interface 'statemgmt.StateDeltaIterator' for details
func (citr *CompositeRangeScanIterator) Close() {}
