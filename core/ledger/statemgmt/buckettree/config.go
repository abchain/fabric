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

	if maxGroupingAtEachLevel < 2 {
		panic("maxgroup number can not be less than 2")
	}

	//the default delta is sqrt(numBucket)
	syncDelta := int(math.Sqrt(float64(numBuckets)))
	//additional configs
	if v, ok := configs[ConfigPartialDelta]; ok {
		syncDelta = cast.ToInt(v)
		logger.Debugf("syncDelta = cast.ToInt(v)	: syncDelta: [%s]", v)
	}

	//syncdelta MUST be adjusted to aligned on group number for a better efficient
	if syncDelta%maxGroupingAtEachLevel != 0 {
		//take the ceiling
		syncDelta = (syncDelta/maxGroupingAtEachLevel + 1) * maxGroupingAtEachLevel
	} else if syncDelta == 0 {
		//panic it!
		panic("syncdelta in 0 is specified")
	}

	logger.Debugf("buckettree: syncDelta is [%d]", syncDelta)

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
		syncDelta:            maxGroupingAtEachLevel,
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

	//if level exceed 128, we have problem in iterating, but even use maxgrouping = 2,
	// 2 ^ 128 just is not a possible number that buckettree can handle
	if currentLevel >= 128 {
		panic("We have levels too large")
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

func (config *config) computeParentPosition(bucketNumber int) (int, int) {
	return (bucketNumber-1)/config.getMaxGroupingAtEachLevel() + 1, (bucketNumber - 1) % config.getMaxGroupingAtEachLevel()
}

func (config *config) computeChildPosition(bucketNumber, childhashindex int) int {
	return (bucketNumber-1)*config.getMaxGroupingAtEachLevel() + childhashindex + 1
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
