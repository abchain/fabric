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

	"github.com/golang/protobuf/proto"
)

type bucketKeyLite struct {
	level        int
	bucketNumber int
}

func (bucketKey *bucketKeyLite) String() string {
	return fmt.Sprintf("level=[%d], bucketNumber=[%d]", bucketKey.level, bucketKey.bucketNumber)
}

func (bucketKey *bucketKeyLite) equals(anotherBucketKey *bucketKeyLite) bool {
	return bucketKey.level == anotherBucketKey.level && bucketKey.bucketNumber == anotherBucketKey.bucketNumber
}

func (bucketKey *bucketKeyLite) clone() *bucketKeyLite {
	return &bucketKeyLite{bucketKey.level, bucketKey.bucketNumber}
}

func (bklite *bucketKeyLite) getBucketKey(conf *config) *bucketKey {
	return &bucketKey{*bklite, conf}
}

func (bucketKey *bucketKeyLite) getChildIndex(childKey *bucketKey) int {
	bucketNumberOfFirstChild := ((bucketKey.bucketNumber - 1) * childKey.getMaxGroupingAtEachLevel()) + 1
	bucketNumberOfLastChild := bucketKey.bucketNumber * childKey.getMaxGroupingAtEachLevel()
	if childKey.bucketNumber < bucketNumberOfFirstChild || childKey.bucketNumber > bucketNumberOfLastChild {
		panic(fmt.Errorf("[%#v] is not a valid child bucket of [%#v]", childKey, bucketKey))
	}
	return childKey.bucketNumber - bucketNumberOfFirstChild
}

type bucketKey struct {
	bucketKeyLite
	*config
}

func newBucketKey(conf *config, level int, bucketNumber int) *bucketKey {
	if level > conf.getLowestLevel() || level < 0 {
		panic(fmt.Errorf("Invalid Level [%d] for bucket key. Level can be between 0 and [%d]", level, conf.getLowestLevel()))
	}

	if bucketNumber < 1 || bucketNumber > conf.getNumBuckets(level) {
		panic(fmt.Errorf("Invalid bucket number [%d]. Bucket nuber at level [%d] can be between 1 and [%d]", bucketNumber, level, conf.getNumBuckets(level)))
	}
	return &bucketKey{bucketKeyLite{level, bucketNumber}, conf}
}

func newBucketKeyAtLowestLevel(conf *config, bucketNumber int) *bucketKey {
	return newBucketKey(conf, conf.getLowestLevel(), bucketNumber)
}

func constructRootBucketKey(conf *config) *bucketKey {
	return newBucketKey(conf, 0, 1)
}

func decodeBucketKey(conf *config, keyBytes []byte) *bucketKey {
	level, numBytesRead := proto.DecodeVarint(keyBytes[1:])
	var bucketNumber uint64
	if conf.newBucketKeyEncoding {
		bkn, _ := decodeBucketNumber(keyBytes[numBytesRead+1:])
		bucketNumber = uint64(bkn)
	} else {
		bucketNumber, _ = proto.DecodeVarint(keyBytes[numBytesRead+1:])
	}

	return newBucketKey(conf, int(level), int(bucketNumber))
}

func (bucketKey *bucketKey) getEncodedBytes() []byte {
	encodedBytes := []byte{0}
	//level is never exceed 128 and it equal to a byte
	//encodedBytes = append(encodedBytes, proto.EncodeVarint(uint64(bucketKey.level))...)
	encodedBytes = append(encodedBytes, byte(bucketKey.level))
	if bucketKey.config.newBucketKeyEncoding {
		encodedBytes = append(encodedBytes, encodeBucketNumber(bucketKey.bucketNumber)...)
	} else {
		encodedBytes = append(encodedBytes, proto.EncodeVarint(uint64(bucketKey.bucketNumber))...)
	}
	return encodedBytes
}

func (bucketKey *bucketKey) getParentKey() *bucketKey {
	return newBucketKey(bucketKey.config, bucketKey.level-1, bucketKey.computeParentBucketNumber(bucketKey.bucketNumber))
}

func (bucketKey *bucketKey) getChildKey(index int) *bucketKey {
	bucketNumberOfFirstChild := ((bucketKey.bucketNumber - 1) * bucketKey.getMaxGroupingAtEachLevel()) + 1
	bucketNumberOfChild := bucketNumberOfFirstChild + index
	return newBucketKey(bucketKey.config, bucketKey.level+1, bucketNumberOfChild)
}

func (bucketKey *bucketKey) equals(anotherBucketKey *bucketKey) bool {
	return bucketKey.bucketKeyLite.equals(&anotherBucketKey.bucketKeyLite)
}
