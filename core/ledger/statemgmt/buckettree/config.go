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
	"github.com/spf13/cast"
	"hash/fnv"
	"math"
)

// ConfigNumBuckets - config name 'numBuckets' as it appears in yaml file
const ConfigNumBuckets = "numbuckets"

// ConfigMaxGroupingAtEachLevel - config name 'maxGroupingAtEachLevel' as it appears in yaml file
const ConfigMaxGroupingAtEachLevel = "maxgroupingateachlevel"

// ConfigHashFunction - config name 'hashFunction'. This is not exposed in yaml file. This configuration is used for testing with custom hash-function
const ConfigHashFunction = "hashfunction"

const ConfigPartialDelta = "syncdelta"

const ConfigBucketCacheMaxSize = "bucketcachesize"

// DefaultNumBuckets - total buckets
const DefaultNumBuckets = 10009

const DefaultSyncDeltaNumBuckets = 5

// DefaultMaxGroupingAtEachLevel - Number of max buckets to group at each level.
// Grouping is started from left. The last group may have less buckets
const DefaultMaxGroupingAtEachLevel = 10

type config struct {
	maxGroupingAtEachLevel int
	lowestLevel            int
	levelToNumBucketsMap   map[int]int
	hashFunc               hashFunc
	syncDelta              int
	bucketCacheMaxSize     int
}

func initConfig(configs map[string]interface{}) *config {
	logger.Infof("configs passed during initialization = %#v", configs)

	numBuckets := DefaultNumBuckets
	if v, ok := configs[ConfigNumBuckets]; ok {
		numBuckets = cast.ToInt(v)
		logger.Debugf("buckettree: numBuckets: [%d]", numBuckets)
	}

	maxGroupingAtEachLevel := DefaultMaxGroupingAtEachLevel
	if v, ok := configs[ConfigMaxGroupingAtEachLevel]; ok {
		maxGroupingAtEachLevel = cast.ToInt(v)
		logger.Debugf("buckettree: maxGroupingAtEachLevel: [%d]", maxGroupingAtEachLevel)
	}

	syncDelta := DefaultSyncDeltaNumBuckets
	//additional configs
	if v, ok := configs[ConfigPartialDelta]; ok {
		syncDelta = cast.ToInt(v)
		logger.Debugf("syncDelta = cast.ToInt(v)	: syncDelta: [%s]", v)
	}
	logger.Debugf("buckettree: syncDelta: [%d]", syncDelta)

	bucketCacheMaxSize := defaultBucketCacheMaxSize
	if v, ok := configs[ConfigBucketCacheMaxSize]; ok {
		bucketCacheMaxSize = cast.ToInt(v)
		logger.Debugf("buckettree: bucketCacheMaxSize: [%d]", bucketCacheMaxSize)
	}

	//TODO: what the hell ...
	hashFunction, ok := configs[ConfigHashFunction].(hashFunc)
	if !ok {
		hashFunction = fnvHash
	}

	conf := newConfig(numBuckets, maxGroupingAtEachLevel)
	conf.syncDelta = syncDelta
	conf.hashFunc = hashFunction
	conf.bucketCacheMaxSize = bucketCacheMaxSize
	logger.Infof("Initializing bucket tree state implemetation with configurations %+v", conf)
	logger.Infof("bucket tree lowestLevel: %+v", conf.lowestLevel)
	logger.Infof("bucket tree levelToNumBucketsMap: %+v", conf.levelToNumBucketsMap)

	return conf
}

func newConfig(numBuckets, maxGroupingAtEachLevel int) *config {
	conf := &config{maxGroupingAtEachLevel: maxGroupingAtEachLevel,
		lowestLevel:          -1,
		levelToNumBucketsMap: make(map[int]int),
		hashFunc:             fnvHash,
	}

	currentLevel := 0
	numBucketAtCurrentLevel := numBuckets
	levelInfoMap := make(map[int]int)
	levelInfoMap[currentLevel] = numBucketAtCurrentLevel
	for numBucketAtCurrentLevel > 1 {
		numBucketAtParentLevel := numBucketAtCurrentLevel / maxGroupingAtEachLevel
		if numBucketAtCurrentLevel%maxGroupingAtEachLevel != 0 {
			numBucketAtParentLevel++
		}

		numBucketAtCurrentLevel = numBucketAtParentLevel
		currentLevel++
		levelInfoMap[currentLevel] = numBucketAtCurrentLevel
	}

	conf.lowestLevel = currentLevel
	for k, v := range levelInfoMap {
		conf.levelToNumBucketsMap[conf.lowestLevel-k] = v
	}
	return conf
}

func (config *config) getNumBuckets(level int) int {
	if level < 0 || level > config.lowestLevel {
		panic(fmt.Errorf("level can only be between 0 and [%d]", config.lowestLevel))
	}
	return config.levelToNumBucketsMap[level]
}

func (config *config) computeBucketHash(data []byte) uint32 {
	return config.hashFunc(data)
}

func (config *config) getLowestLevel() int {
	return config.lowestLevel
}

func (config *config) getDelta(level int) uint64 {
	return min(uint64(config.syncDelta), uint64(config.getNumBuckets(level)))
}

func (config *config) getMaxGroupingAtEachLevel() int {
	return config.maxGroupingAtEachLevel
}

func (config *config) getNumBucketsAtLowestLevel() int {
	return config.getNumBuckets(config.getLowestLevel())
}

func (config *config) computeParentBucketNumber(bucketNumber int) int {
	logger.Debugf("Computing parent bucket number for bucketNumber [%d]", bucketNumber)
	parentBucketNumber := bucketNumber / config.getMaxGroupingAtEachLevel()
	if bucketNumber%config.getMaxGroupingAtEachLevel() != 0 {
		parentBucketNumber++
	}
	return parentBucketNumber
}

//a "represent node" is the lowest sole node which just contain all the child node specified in arguments
//return nil for there no any represent node can be calculated (for example, a delta larger than max nodes number
//in that level)
func (config *config) getRepresentNode(lvl int, blkNumber uint64, delta uint64) *bucketKey {

	if delta == 0 || int(delta) > config.getNumBuckets(lvl) {
		logger.Errorf("have wrong  arg in get RepresentNode: [%d, %d, %d]", lvl, blkNumber, delta)
		return nil
	}

	//sanity check: the blkNumber must not 0
	if blkNumber == 0 {
		panic("malform arg of blkNumber")
	}

	lvlgroup := config.getMaxGroupingAtEachLevel()
	//obtained the 0-start indexed, closed interval
	begNum := int(blkNumber) - 1
	endNum := int(blkNumber+delta) - 1

	for i := lvl; i >= 0; i-- {
		if endNum == begNum {
			return newBucketKey(config, i, begNum+1)
		}
		begNum = begNum / lvlgroup
		endNum = endNum / lvlgroup
	}

	return nil
}

// func (config *config) GetNumBuckets(level int) int {
// 	return config.getNumBuckets(level)
// }
// func (config *config) GetLowestLevel() int {
// 	return config.lowestLevel
// }
func (config *config) getSyncLevel() int {
	return config.lowestLevel - 1
}

func (config *config) Verify(level, startBucket, endBucket int) error {
	if level > config.lowestLevel {
		return fmt.Errorf("Invalid level")
	}

	if endBucket > config.getNumBuckets(level) {
		return fmt.Errorf("Invalid end bucket num")
	}

	if startBucket > endBucket {
		return fmt.Errorf("Invalid start bucket num")
	}

	return nil
}

// func (c *config) getLeafBuckets(level int, bucketNum int) (start, end int) {

// 	if level > c.lowestLevel || bucketNum > c.getNumBuckets(level) {
// 		return 0, 0
// 	}

// 	res := pow(c.maxGroupingAtEachLevel, c.lowestLevel-level)

// 	end = int(res) * bucketNum
// 	if numbuckets := c.getNumBucketsAtLowestLevel(); end > numbuckets {
// 		end = c.numbuckets
// 	}

// 	start = 1 + (bucketNum-1)*int(res)
// 	logger.Debugf("LeafBuckets: %d-%d\n", start, end)
// 	return start, end
// }

func (conf *config) getNecessaryBuckets(level int, bucketNum int) []*bucketKey {

	bucketKeyList := make([]*bucketKey, 0)
	if level > conf.lowestLevel {
		return bucketKeyList
	}

	baseNumber := conf.maxGroupingAtEachLevel
	antilogarithm := bucketNum
	offset := 0

	for {
		logarithmic := log(antilogarithm, baseNumber)
		if pow(baseNumber, logarithmic+1) <= antilogarithm {
			logarithmic += 1
		}
		deltaOffset := pow(baseNumber, logarithmic)
		offset += deltaOffset
		bk := newBucketKey(conf, level-logarithmic, offset/deltaOffset)
		bucketKeyList = append(bucketKeyList, bk)

		logger.Debugf("bucketKey[%s], delta level[%d], offset[%d]\n", bk, logarithmic, offset)
		antilogarithm -= deltaOffset
		if antilogarithm < baseNumber {
			for offset < bucketNum {
				bk := newBucketKey(conf, level, offset+1)
				bucketKeyList = append(bucketKeyList, bk)
				logger.Debugf("bucketKey[%s]\n", bk)
				offset++
			}
			break
		}
	}
	return bucketKeyList
}

type hashFunc func(data []byte) uint32

func fnvHash(data []byte) uint32 {
	fnvHash := fnv.New32a()
	fnvHash.Write(data)
	return fnvHash.Sum32()
}

func pow(a, b int) int {
	return int(math.Pow(float64(a), float64(b)))
}

func log(antilogarithm, baseNumber int) int {
	logarithm := math.Log(float64(antilogarithm)) / math.Log(float64(baseNumber))
	return int(math.Floor(logarithm))
}

func min(x, y uint64) uint64 {
	if x < y {
		return x
	}
	return y
}
