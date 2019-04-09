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

package buckettree

import (
	"fmt"

	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/core/ledger/util"
	//"github.com/golang/protobuf/proto"
)

type dataKey struct {
	bucketNumber int
	compositeKey []byte
}

// YA-fabric: we use new encoding for datakey, the original way make a max byte as 8
// so we use a larger (15) byte as the prefix
const dataKeyPrefixByte = byte(15)

func newDataKey(conf *config, chaincodeID string, key string) *dataKey {
	logger.Debugf("Enter - newDataKey. chaincodeID=[%s], key=[%s]", chaincodeID, key)
	compositeKey := statemgmt.ConstructCompositeKey(chaincodeID, key)
	bucketHash := conf.computeBucketHash(compositeKey)
	// Adding one because - we start bucket-numbers 1 onwards
	bucketNumber := int(bucketHash)%conf.getNumBucketsAtLowestLevel() + 1
	dataKey := &dataKey{bucketNumber, compositeKey}
	logger.Debugf("Exit - newDataKey=[%s]", dataKey)
	return dataKey
}

func minimumPossibleDataKeyBytesFor(bucketKey *bucketKey) []byte {
	//TODO: must calc or the verify the number is for lowest level
	min := encodeBucketNumber(bucketKey.bucketNumber)
	min = append(min, byte(0))
	return min
}

func minimumPossibleDataKeyBytes(bucketNumber int, chaincodeID string, key string) []byte {
	b := encodeBucketNumber(bucketNumber)
	b = append(b, statemgmt.ConstructCompositeKey(chaincodeID, key)...)
	return b
}

func (key *dataKey) getBucketKey(conf *config) *bucketKeyLite {
	return &newBucketKeyAtLowestLevel(conf, key.bucketNumber).bucketKeyLite
}

func encodeBucketNumber(bucketNumber int) []byte {
	return util.EncodeOrderPreservingVarUint64(uint64(bucketNumber))
}

func decodeBucketNumber(encodedBytes []byte) (int, int) {

	if len(encodedBytes) == 0 {
		return 0, 0
	} else {
		bucketNum, bytesConsumed := util.DecodeOrderPreservingVarUint64(encodedBytes)
		return int(bucketNum), bytesConsumed
	}
}

func (key *dataKey) getEncodedBytes() []byte {
	encodedBytes := encodeBucketNumber(key.bucketNumber)
	encodedBytes = append(encodedBytes, key.compositeKey...)
	return encodedBytes
}

func newDataKeyFromEncodedBytes(encodedBytes []byte) *dataKey {
	bucketNum, l := decodeBucketNumber(encodedBytes)
	compositeKey := encodedBytes[l:]
	return &dataKey{bucketNum, compositeKey}
}

func (key *dataKey) String() string {
	return fmt.Sprintf("bucketNum=[%v], compositeKey=[%8x]", key.bucketNumber, key.compositeKey)
}

func (key *dataKey) clone() *dataKey {
	clone := &dataKey{key.bucketNumber, key.compositeKey}
	return clone
}
