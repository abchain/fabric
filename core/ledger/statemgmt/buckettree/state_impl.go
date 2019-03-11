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
	"bytes"
	"github.com/golang/protobuf/proto"

	"fmt"
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	pb "github.com/abchain/fabric/protos"
	"github.com/op/go-logging"
)

var logger = logging.MustGetLogger("buckettree")

// StateImpl - implements the interface - 'statemgmt.HashableState'
type StateImpl struct {
	*db.OpenchainDB
	currentConfig          *config
	dataNodesDelta         *dataNodesDelta  // bucket nodes map  level-bucketNum -> node, stores users key-value
	bucketTreeDelta        *bucketTreeDelta // bucket tree, each node contains all its child hash
	persistedStateHash     []byte
	lastComputedCryptoHash []byte
	recomputeCryptoHash    bool
	underSync              *syncProcess
	bucketCache            *bucketCache
}

// NewStateImpl constructs a new StateImpl
func NewStateImpl(db *db.OpenchainDB) *StateImpl {
	return &StateImpl{OpenchainDB: db}
}

// Initialize - method implementation for interface 'statemgmt.HashableState'
func (stateImpl *StateImpl) Initialize(configs map[string]interface{}) error {
	stateImpl.currentConfig = initConfig(configs)
	rootBucketNode, err := fetchBucketNodeFromDB(stateImpl.OpenchainDB, constructRootBucketKey(stateImpl.currentConfig))
	if err != nil {
		return err
	}
	if rootBucketNode != nil {
		stateImpl.persistedStateHash = rootBucketNode.computeCryptoHash()
		stateImpl.lastComputedCryptoHash = stateImpl.persistedStateHash
	}

	stateImpl.bucketCache = newBucketCache(stateImpl.currentConfig.bucketCacheMaxSize, stateImpl.OpenchainDB)
	stateImpl.bucketCache.loadAllBucketNodesFromDB(stateImpl.currentConfig)
	stateImpl.underSync = checkSyncProcess(stateImpl)

	if stateImpl.underSync != nil {
		return stateImpl.underSync
	} else {
		return nil
	}

}

// Get - method implementation for interface 'statemgmt.HashableState'
func (stateImpl *StateImpl) Get(chaincodeID string, key string) ([]byte, error) {
	dataKey := newDataKey(stateImpl.currentConfig, chaincodeID, key)
	dataNode, err := fetchDataNodeFromDB(stateImpl.OpenchainDB, dataKey)
	if err != nil {
		return nil, err
	}
	if dataNode == nil {
		return nil, nil
	}
	return dataNode.value, nil
}

func (stateImpl *StateImpl) GetSafe(sn *db.DBSnapshot, _ int, chaincodeID string, key string) ([]byte, error) {
	dataKey := newDataKey(stateImpl.currentConfig, chaincodeID, key)
	dataNode, err := fetchDataNodeFromSnapshot(sn, dataKey)
	if err != nil {
		return nil, err
	}
	if dataNode == nil {
		return nil, nil
	}
	return dataNode.value, nil
}

// PrepareWorkingSet - method implementation for interface 'statemgmt.HashableState'
func (stateImpl *StateImpl) PrepareWorkingSet(stateDelta *statemgmt.StateDelta) error {
	logger.Debug("Enter - PrepareWorkingSet()")
	if stateDelta.IsEmpty() {
		logger.Debug("Ignoring working-set as it is empty")
		return nil
	}
	stateImpl.dataNodesDelta = newDataNodesDelta(stateImpl.currentConfig, stateDelta)
	stateImpl.bucketTreeDelta = newBucketTreeDelta()
	stateImpl.recomputeCryptoHash = true
	return nil
}

// ClearWorkingSet - method implementation for interface 'statemgmt.HashableState'
func (stateImpl *StateImpl) ClearWorkingSet(changesPersisted bool) {
	logger.Debug("Enter - ClearWorkingSet()")
	if changesPersisted {
		stateImpl.persistedStateHash = stateImpl.lastComputedCryptoHash
		stateImpl.updateBucketCache()
	} else {
		stateImpl.lastComputedCryptoHash = stateImpl.persistedStateHash
	}
	stateImpl.dataNodesDelta = nil
	stateImpl.bucketTreeDelta = nil
	stateImpl.recomputeCryptoHash = false
}

// ComputeCryptoHash - method implementation for interface 'statemgmt.HashableState'
func (stateImpl *StateImpl) ComputeCryptoHash() ([]byte, error) {
	logger.Debug("Enter - ComputeCryptoHash()")
	if stateImpl.recomputeCryptoHash {
		logger.Debug("Recomputing crypto-hash...")
		err := stateImpl.processDataNodeDelta() // feed all leaf nodes(level n) and all their parent nodes(level n-1)
		if err != nil {
			return nil, err
		}
		err = stateImpl.processBucketTreeDelta(0) // feed all other nodes(level n-2 to level 0)
		if err != nil {
			return nil, err
		}
		stateImpl.lastComputedCryptoHash = stateImpl.computeRootNodeCryptoHash()
		stateImpl.recomputeCryptoHash = false
	} else {
		logger.Debug("Returing existing crypto-hash as recomputation not required")
	}
	return stateImpl.lastComputedCryptoHash, nil
}

func (stateImpl *StateImpl) processDataNodeDelta() error {
	afftectedBuckets := stateImpl.dataNodesDelta.getAffectedBuckets()
	for _, bucketKeyLite := range afftectedBuckets {
		bucketKey := bucketKeyLite.getBucketKey(stateImpl.currentConfig)
		updatedDataNodes := stateImpl.dataNodesDelta.getSortedDataNodesFor(bucketKeyLite)
		existingDataNodes, err := fetchDataNodesFromDBFor(stateImpl.OpenchainDB, bucketKey)
		if err != nil {
			return err
		}
		cryptoHashForBucket := computeDataNodesCryptoHash(bucketKey, updatedDataNodes, existingDataNodes)
		logger.Debugf("Crypto-hash for lowest-level bucket [%s] is [%x]", bucketKey, cryptoHashForBucket)
		parentBucket := stateImpl.bucketTreeDelta.getOrCreateBucketNode(bucketKey.getParentKey())
		logger.Debugf("Feed DataNode<%s> to parentBucket [%s]", bucketKey, &parentBucket.bucketKey)
		// set second last level children hash by index
		parentBucket.setChildCryptoHash(bucketKey, cryptoHashForBucket)
	}
	return nil
}

func (stateImpl *StateImpl) processBucketTreeDelta(tillLevel int) error {
	secondLastLevel := stateImpl.currentConfig.getLowestLevel() - 1
	for level := secondLastLevel; level >= tillLevel; level-- {
		bucketNodes := stateImpl.bucketTreeDelta.getBucketNodesAt(level)
		logger.Debugf("Bucket tree delta. Number of buckets at level [%d] are [%d]", level, len(bucketNodes))
		for _, bucketNode := range bucketNodes {
			logger.Debugf("bucketNode in tree-delta [%s]", bucketNode)
			bucketKey := bucketNode.bucketKey.getBucketKey(stateImpl.currentConfig)
			// get middle node from db
			dbBucketNode, err := stateImpl.bucketCache.get(bucketKey)
			logger.Debugf("bucket node from db [%s]", dbBucketNode)
			if err != nil {
				return err
			}

			// merge updated child hash into middle node by index
			if dbBucketNode != nil {
				bucketNode.mergeBucketNode(dbBucketNode)
				logger.Debugf("After merge... bucketNode in tree-delta [%s]", bucketNode)
			}
			if level == 0 {
				return nil
			}
			logger.Debugf("Computing cryptoHash for bucket [%s]", bucketNode)
			cryptoHash := bucketNode.computeCryptoHash()

			logger.Debugf("cryptoHash for bucket [%s] is [%x]", bucketKey, cryptoHash)
			parentBucket := stateImpl.bucketTreeDelta.getOrCreateBucketNode(bucketKey.getParentKey())

			logger.Debugf("Feed bucketNode <%s> to parentBucket [%+v]", &bucketNode.bucketKey, parentBucket.bucketKey)
			parentBucket.setChildCryptoHash(bucketKey, cryptoHash)
		}
	}
	return nil
}

func (stateImpl *StateImpl) computeRootNodeCryptoHash() []byte {
	return stateImpl.bucketTreeDelta.getRootNode().computeCryptoHash()
}

// compute a leaf bucket hash by updatedNodes and existingNodes
func computeDataNodesCryptoHash(bucketKey *bucketKey, updatedNodes dataNodes, existingNodes dataNodes) []byte {
	logger.Debugf("Computing crypto-hash for bucket [%s]. numUpdatedNodes=[%d], numExistingNodes=[%d]",
		bucketKey, len(updatedNodes), len(existingNodes))

	bucketHashCalculator := newBucketHashCalculator(bucketKey)
	i := 0
	j := 0
	for i < len(updatedNodes) && j < len(existingNodes) {
		updatedNode := updatedNodes[i]
		existingNode := existingNodes[j]
		c := bytes.Compare(updatedNode.dataKey.compositeKey, existingNode.dataKey.compositeKey)
		var nextNode *dataNode
		switch c {
		case -1:
			nextNode = updatedNode
			i++
		case 0:
			nextNode = updatedNode
			i++
			j++
		case 1:
			nextNode = existingNode
			j++
		}
		if !nextNode.isDelete() {
			bucketHashCalculator.addNextNode(nextNode)
		}
	}

	var remainingNodes dataNodes
	if i < len(updatedNodes) {
		remainingNodes = updatedNodes[i:]
	} else if j < len(existingNodes) {
		remainingNodes = existingNodes[j:]
	}

	for _, remainingNode := range remainingNodes {
		if !remainingNode.isDelete() {
			bucketHashCalculator.addNextNode(remainingNode)
		}
	}
	return bucketHashCalculator.computeCryptoHash()
}

// AddChangesForPersistence - method implementation for interface 'statemgmt.HashableState'
func (stateImpl *StateImpl) AddChangesForPersistence(writeBatch *db.DBWriteBatch) error {

	if stateImpl.dataNodesDelta == nil && stateImpl.underSync == nil {
		return nil
	}

	if stateImpl.dataNodesDelta != nil {

		if stateImpl.recomputeCryptoHash {
			_, err := stateImpl.ComputeCryptoHash()
			if err != nil {
				return err
			}
		}
		stateImpl.addDataNodeChangesForPersistence(writeBatch)
	}

	stateImpl.addBucketNodeChangesForPersistence(writeBatch)
	if stateImpl.underSync != nil {
		err := stateImpl.underSync.PersistProgress(writeBatch)
		if err != nil {
			return err
		}
	}

	return nil
}

func (stateImpl *StateImpl) addDataNodeChangesForPersistence(writeBatch *db.DBWriteBatch) {
	openchainDB := writeBatch.GetDBHandle()
	affectedBuckets := stateImpl.dataNodesDelta.getAffectedBuckets()
	for _, affectedBucket := range affectedBuckets {
		dataNodes := stateImpl.dataNodesDelta.getSortedDataNodesFor(affectedBucket)
		for _, dataNode := range dataNodes {
			if dataNode.isDelete() {
				logger.Debugf("Deleting data node key = %#v", dataNode.dataKey)
				writeBatch.DeleteCF(openchainDB.StateCF, dataNode.dataKey.getEncodedBytes())
			} else {
				logger.Debugf("Adding data node with value = %#v", dataNode.value)
				writeBatch.PutCF(openchainDB.StateCF, dataNode.dataKey.getEncodedBytes(), dataNode.value)
			}
		}
	}
}

func (stateImpl *StateImpl) addBucketNodeChangesForPersistence(writeBatch *db.DBWriteBatch) {
	openchainDB := writeBatch.GetDBHandle()
	secondLastLevel := stateImpl.currentConfig.getLowestLevel() - 1
	for level := secondLastLevel; level >= 0; level-- {
		bucketNodes := stateImpl.bucketTreeDelta.getBucketNodesAt(level)
		for _, bucketNode := range bucketNodes {
			if bucketNode.markedForDeletion {
				writeBatch.DeleteCF(openchainDB.StateCF, bucketNode.bucketKey.getEncodedBytes())
			} else {

				logger.Debugf("Persist bucketNode<%+v>, dataKey<%x>, value <%x>",
					bucketNode.bucketKey,
					bucketNode.bucketKey.getEncodedBytes(),
					bucketNode.marshal())

				writeBatch.PutCF(openchainDB.StateCF,
					bucketNode.bucketKey.getEncodedBytes(), bucketNode.marshal())
			}
		}
	}
}

func (stateImpl *StateImpl) updateBucketCache() {
	if stateImpl.bucketTreeDelta == nil || stateImpl.bucketTreeDelta.isEmpty() {
		return
	}
	stateImpl.bucketCache.lock.Lock()
	defer stateImpl.bucketCache.lock.Unlock()
	secondLastLevel := stateImpl.currentConfig.getLowestLevel() - 1
	for level := 0; level <= secondLastLevel; level++ {
		bucketNodes := stateImpl.bucketTreeDelta.getBucketNodesAt(level)
		for _, bucketNode := range bucketNodes {
			key := bucketNode.bucketKey
			if bucketNode.markedForDeletion {
				stateImpl.bucketCache.removeWithoutLock(key)
			} else {
				stateImpl.bucketCache.putWithoutLock(key, bucketNode)
			}
		}
	}
}

// PerfHintKeyChanged - method implementation for interface 'statemgmt.HashableState'
func (stateImpl *StateImpl) PerfHintKeyChanged(chaincodeID string, key string) {
	// We can create a cache. Pull all the keys for the bucket (to which given key belongs) in a separate thread
	// This prefetching can help making method 'ComputeCryptoHash' faster.
}

// GetStateSnapshotIterator - method implementation for interface 'statemgmt.HashableState'
func (stateImpl *StateImpl) GetStateSnapshotIterator(snapshot *db.DBSnapshot) (statemgmt.StateSnapshotIterator, error) {
	return newStateSnapshotIterator(snapshot)
}

// GetRangeScanIterator - method implementation for interface 'statemgmt.HashableState'
func (stateImpl *StateImpl) GetRangeScanIterator(chaincodeID string, startKey string, endKey string) (statemgmt.RangeScanIterator, error) {
	return newRangeScanIterator(stateImpl.OpenchainDB, chaincodeID, startKey, endKey)
}

// ---- HashAndDividableState interface -----

func (stateImpl *StateImpl) GetPartialRangeIterator(snapshot *db.DBSnapshot) (statemgmt.PartialRangeIterator, error) {
	if stateImpl.currentConfig == nil {
		return nil, fmt.Errorf("Not inited")
	}

	return newPartialSnapshotIterator(snapshot, stateImpl.currentConfig)
}

func (stateImpl *StateImpl) InitPartialSync(statehash []byte) {
	stateImpl.underSync = newSyncProcess(stateImpl, statehash)
	//clear bucket cache
	stateImpl.lastComputedCryptoHash = statehash
	stateImpl.persistedStateHash = nil
	stateImpl.dataNodesDelta = nil
	stateImpl.bucketTreeDelta = nil
	stateImpl.recomputeCryptoHash = true
	stateImpl.bucketCache = newBucketCache(stateImpl.currentConfig.bucketCacheMaxSize, stateImpl.OpenchainDB)

	logger.Infof("start sync to state %x", statehash)
}

func (stateImpl *StateImpl) IsCompleted() bool {
	return stateImpl.underSync == nil
}

func (stateImpl *StateImpl) RequiredParts() ([]*pb.SyncOffset, error) {
	if stateImpl.underSync == nil {
		return nil, fmt.Errorf("Not under syncing progress")
	}

	return stateImpl.underSync.RequiredParts()
}

//An PrepareWorkingSet must have been called before, we do this like a calling of
//ClearWorkingSet(true), verify the delta
func (stateImpl *StateImpl) ApplyPartialSync(syncData *pb.SyncStateChunk) error {

	if stateImpl.underSync == nil {
		return fmt.Errorf("Not under syncing progress")
	}

	offset, err := syncData.GetOffset().Unmarshal2BucketTree()
	if err != nil {
		return err
	}

	representNode := stateImpl.currentConfig.getRepresentNode(int(offset.Level), offset.BucketNum, offset.Delta)
	if representNode == nil {
		return fmt.Errorf("Not a valid represent for range [%v], abandon it", offset)
	}
	logger.Infof("---- ApplyPartialSync offset [%v]  -----", offset)

	if md := syncData.GetMetaData(); len(md) > 0 {
		if err := stateImpl.applyPartialMetalData(md, offset); err != nil {
			return err
		}
	}

	if err := stateImpl.processDataNodeDelta(); err != nil {
		return err
	}

	if err := stateImpl.processBucketTreeDelta(representNode.level); err != nil {
		return err
	}

	verifiedErr := fmt.Errorf("represent node [%s] is not found in delta", representNode)
	//verify the hash we calculated with which it obatined
	if lvld, ok := stateImpl.bucketTreeDelta.byLevel[representNode.level]; ok {
		if buck, ok := lvld[representNode.bucketNumber]; ok {
			if cachedbuck, err := stateImpl.bucketCache.get(representNode); err != nil {
				verifiedErr = fmt.Errorf("no record for represent node [%s]: %s", representNode, err)
			} else {

				buckH := buck.computeCryptoHash()
				verifiedH := cachedbuck.computeCryptoHash()
				if bytes.Compare(buckH, verifiedH) != 0 {
					verifiedErr = fmt.Errorf("Verify fail! get [%x], expect [%x]", buckH, verifiedH)
				} else {
					//finally we are DONE
					verifiedErr = nil
				}
			}
		}
	}

	if verifiedErr != nil {
		return verifiedErr
	}

	if err := stateImpl.underSync.CompletePart(offset); err != nil {
		return err
	}

	if stateImpl.underSync.current == nil {
		logger.Infof("---- Syncing to state [%x] finish -----", stateImpl.underSync.targetStateHash)
		stateImpl.underSync = nil
	}

	return nil
}

func (stateImpl *StateImpl) applyPartialMetalData(md []byte, offset *pb.BucketTreeOffset) error {

	logger.Infof("Start: applyPartialMetalData: offset[%+v]", offset)
	defer logger.Infof("Fnished: applyPartialMetalData: offset[%+v]", offset)

	metadata := &pb.SyncMetadata{}
	err := proto.Unmarshal(md, metadata)

	if err != nil {
		return err
	}

	if stateImpl.bucketTreeDelta == nil {
		stateImpl.bucketTreeDelta = newBucketTreeDelta()
	}

	treeDelta := stateImpl.bucketTreeDelta
	byBucketNumber := make(map[int]*bucketNode)
	treeDelta.byLevel[int(offset.Level)] = byBucketNumber

	//if list has data more than delta specified, we trunctate it (so save cost)
	//but we don't care a shorter list than delta for verify process can handle that
	if len(metadata.BucketNodeHashList) > int(offset.Delta) {
		metadata.BucketNodeHashList = metadata.BucketNodeHashList[:offset.Delta]
	}

	for i, hashes := range metadata.BucketNodeHashList {
		bucketNum := int(offset.BucketNum) + i
		bk := newBucketKey(stateImpl.currentConfig, int(offset.Level), bucketNum)
		unmarshaledBucketNode := unmarshalBucketNode(bk, hashes)
		if unmarshaledBucketNode == nil {
			continue
		}
		byBucketNumber[bucketNum] = unmarshaledBucketNode
		logger.Debugf("Recv metadata: bucketNode[%v]", unmarshaledBucketNode)
	}

	return nil
}
