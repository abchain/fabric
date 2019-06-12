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

//notice the general snapshotstate is not related to a stateimpl object
//it just make use of the config in stateimpl for creating
func (stateImpl *StateImpl) NewSnapshotState() statemgmt.SnapshotState {
	if stateImpl.currentConfig == nil {
		panic("impl not inited")
	}
	return snapshotGen{stateImpl.currentConfig}
}

// Initialize - method implementation for interface 'statemgmt.HashableState'
func (stateImpl *StateImpl) Initialize(configs map[string]interface{}) error {

	//if this flag is set (no matter what value it is), always load all settings
	//from config file
	_, forceLoad := configs["forceload"]

	//load config first, which will replace the configs in argument
	if cfgSaved, err := stateImpl.GetValue(db.StateCF, configDataKey); err != nil {
		return err
	} else if len(cfgSaved) != 0 && !forceLoad {
		stateImpl.currentConfig, err = loadconfig(cfgSaved, configs)
		if err != nil {
			return fmt.Errorf("loading config fail: %s", err)
		}
	} else {
		stateImpl.currentConfig = initConfig(configs)
		persisted, err := stateImpl.currentConfig.persist()
		if err == nil {
			err = stateImpl.PutValue(db.StateCF, configDataKey, persisted)
		}

		if err != nil {
			return fmt.Errorf("saving config fail: %s", err)
		}
	}

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

	if syncSaved, err := stateImpl.GetValue(db.StateCF, partialStatusKey); err != nil {
		return fmt.Errorf("reading syncing state fail: %s", err)
	} else if len(syncSaved) != 0 {

		stateImpl.underSync, err = loadSyncProcess(stateImpl, syncSaved)
		if err != nil {
			return fmt.Errorf("loading syncing state fail: %s", err)
		}
		stateImpl.lastComputedCryptoHash = stateImpl.underSync.targetStateHash
		stateImpl.recomputeCryptoHash = false
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

func (stateImpl *StateImpl) GetSafe(sn *db.DBSnapshot, chaincodeID string, key string) ([]byte, error) {
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
	if stateImpl.underSync == nil {
		stateImpl.recomputeCryptoHash = true
	}

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
	if stateImpl.dataNodesDelta == nil {
		return nil
	}
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
	if stateImpl.bucketTreeDelta == nil {
		return nil
	}

	secondLastLevel := stateImpl.currentConfig.getLowestLevel() - 1
	for level := secondLastLevel; level >= tillLevel; level-- {
		bucketNodes := stateImpl.bucketTreeDelta.getBucketNodesAt(level)
		logger.Debugf("Bucket tree delta. Number of buckets at level [%d] are [%d]", level, len(bucketNodes))
		for _, bucketNode := range bucketNodes {
			logger.Debugf("bucketNode in tree-delta [%s]", bucketNode)
			bucketKey := bucketNode.bucketKey.getBucketKey(stateImpl.currentConfig)

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
				continue
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
		bt, err := stateImpl.underSync.PersistProgress()
		if err != nil {
			return err
		}
		writeBatch.PutCF(writeBatch.GetDBHandle().StateCF, partialStatusKey, bt)
	}

	return nil
}

func (stateImpl *StateImpl) addDataNodeChangesForPersistence(writeBatch *db.DBWriteBatch) {
	if stateImpl.dataNodesDelta == nil {
		return
	}
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
	if stateImpl.bucketTreeDelta == nil {
		return
	}
	openchainDB := writeBatch.GetDBHandle()
	secondLastLevel := stateImpl.currentConfig.getLowestLevel() - 1
	for level := secondLastLevel; level >= 0; level-- {
		bucketNodes := stateImpl.bucketTreeDelta.getBucketNodesAt(level)
		for _, bucketNode := range bucketNodes {
			bk := bucketNode.bucketKey.getBucketKey(stateImpl.currentConfig)
			if bucketNode.markedForDeletion {
				writeBatch.DeleteCF(openchainDB.StateCF, bk.getEncodedBytes())
			} else {

				bkByte := bk.getEncodedBytes()
				logger.Debugf("Persist bucketNode<%+v>, dataKey<%x>, value <%x>",
					bucketNode.bucketKey,
					bkByte,
					bucketNode.marshal())

				writeBatch.PutCF(openchainDB.StateCF, bkByte, bucketNode.marshal())
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

	if !stateImpl.IsCompleted() {
		logger.Warningf("processing syncing [to target %X] is pruned", stateImpl.underSync.targetStateHash)
	}

	stateImpl.underSync = newSyncProcess(stateImpl, statehash)
	//clear bucket cache
	stateImpl.lastComputedCryptoHash = statehash
	stateImpl.persistedStateHash = nil
	stateImpl.dataNodesDelta = nil
	stateImpl.bucketTreeDelta = nil
	stateImpl.recomputeCryptoHash = false
	stateImpl.bucketCache = newBucketCache(stateImpl.currentConfig.bucketCacheMaxSize, stateImpl.OpenchainDB)

	logger.Infof("start sync to state %x", statehash)
}

func (stateImpl *StateImpl) IsCompleted() bool {
	return stateImpl.underSync == nil
}

func (stateImpl *StateImpl) SyncTarget() []byte {
	if stateImpl.underSync == nil {
		return nil
	}

	return stateImpl.underSync.targetStateHash
}

func (stateImpl *StateImpl) RequiredParts() ([]*pb.SyncOffset, error) {
	if stateImpl.underSync == nil {
		return nil, nil
	}

	return stateImpl.underSync.RequiredParts()
}

//An PrepareWorkingSet must have been called before, we do this like a calling of
//ClearWorkingSet(true), verify the delta
func (stateImpl *StateImpl) ApplyPartialSync(syncData *pb.SyncStateChunk) error {

	if stateImpl.underSync == nil {
		return fmt.Errorf("Not under syncing progress")
	}

	offset := syncData.GetOffset().GetBuckettree()
	if offset == nil {
		return fmt.Errorf("chunk [%v] has no content for buckettree", syncData)
	}

	//forbid unexpected range ...
	if cur := stateImpl.underSync.current; offset.GetLevel() != cur.GetLevel() || offset.GetBucketNum() != cur.GetBucketNum() {
		return fmt.Errorf("Unexpected range, expected [%v] but have [%v]", cur, offset)
	}

	// representNode := stateImpl.currentConfig.getRepresentNode(int(offset.Level), offset.BucketNum, offset.Delta)
	// if representNode == nil {
	// 	return fmt.Errorf("Not a valid represent for range [%v], abandon it", offset)
	// }
	logger.Infof("---- ApplyPartialSync offset [%v]  -----", offset)

	if err := stateImpl.applyPartialMetaData(syncData.GetMetaData(), offset); err != nil {
		return err
	}

	vlevel := stateImpl.underSync.verifyLevel
	logger.Debugf("---- will verify current level on %d", vlevel)

	if err := stateImpl.processDataNodeDelta(); err != nil {
		return err
	}

	if err := stateImpl.processBucketTreeDelta(vlevel + 1); err != nil {
		return err
	}

	//first verification, which is special, we calc root for verification
	if vlevel == -1 {

		if stateImpl.bucketTreeDelta == nil {
			return fmt.Errorf("Verify fail for no content in root-updating step")
		} else if rn := stateImpl.bucketTreeDelta.getRootNodeSafe(); rn == nil {
			return fmt.Errorf("Verify fail for no content in root-updating step")
		} else if rhash := rn.computeCryptoHash(); bytes.Compare(
			rhash, stateImpl.underSync.targetStateHash) != 0 {
			return fmt.Errorf("Verify fail on root: expected root hash [%X] but get [%X]",
				rhash, stateImpl.underSync.targetStateHash)
		} else {
			//done, and we set statehash now
			stateImpl.lastComputedCryptoHash = rhash
		}

	} else {

		var vbucketLevel byBucketNumber
		if stateImpl.bucketTreeDelta != nil {
			vbucketLevel = stateImpl.bucketTreeDelta.byLevel[vlevel]
			//notice: data in this level is not correct (only part of the childhash) and can not be
			//merged into cache
			defer delete(stateImpl.bucketTreeDelta.byLevel, vlevel)
		}
		if vbucketLevel == nil {
			vbucketLevel = byBucketNumber(map[int]*bucketNode{})
		}

		verifyRange := stateImpl.underSync.verifiedRange()
		//first, we check out the buckets in verify level must in verify range
		for ind, node := range vbucketLevel {
			for i, hash := range node.childrenCryptoHash {
				if pos := stateImpl.currentConfig.computeChildPosition(ind, i); (pos < verifyRange[0] || pos > verifyRange[1]) && len(hash) != 0 {
					return fmt.Errorf("found polluted bucket %d (at level %d), (should %v)", pos, vlevel, verifyRange)
				}
			}
		}

		var cachedbuck *bucketNode
		var representK *bucketKey
		for i := verifyRange[0]; i <= verifyRange[1]; i++ {
			ind, child := stateImpl.currentConfig.computeParentPosition(i)
			if representK == nil || representK.bucketNumber != ind {
				representK = newBucketKey(stateImpl.currentConfig, vlevel, ind)
				cachedbuck, _ = stateImpl.bucketCache.get(representK)
			}

			checkedN := vbucketLevel[ind]
			var ch, vh []byte
			if cachedbuck != nil {
				vh = cachedbuck.childrenCryptoHash[child]
			}

			if checkedN != nil {
				ch = checkedN.childrenCryptoHash[child]
			}

			if bytes.Compare(ch, vh) != 0 {
				return fmt.Errorf("Verify fail @%d(<%s>/%d): expected buckethash [%X] but get [%X]", i, representK, child, vh, ch)
			}
			//ok, we finally passed
		}
	}

	if err := stateImpl.underSync.CompletePart(offset); err != nil {
		return err
	}

	if stateImpl.underSync.current == nil {
		//we has got lastcomputedcryptohash when verify level is -1
		logger.Infof("---- Syncing to state [%x] finish -----", stateImpl.lastComputedCryptoHash)
		stateImpl.underSync = nil
		stateImpl.DeleteKey(db.StateCF, partialStatusKey)
	}

	return nil
}

func (stateImpl *StateImpl) applyPartialMetaData(metadata *pb.SyncMetadata, offset *pb.BucketTreeOffset) error {

	// logger.Infof("Start: applyPartialMetalData: offset[%+v]", offset)
	// defer logger.Infof("Fnished: applyPartialMetalData: offset[%+v]", offset)

	nodes := metadata.GetBuckettree()

	if nodes == nil {
		//nothing to apply
		return nil
	}

	if stateImpl.bucketTreeDelta == nil {
		stateImpl.bucketTreeDelta = newBucketTreeDelta()
	}

	//if list has data more than delta specified, we trunctate it (so save cost)
	if len(nodes.Nodes) > int(offset.Delta) {
		nodes.Nodes = nodes.Nodes[:offset.Delta]
	}

	for _, node := range nodes.Nodes {

		if node.BucketNum < offset.BucketNum || node.Level != offset.Level {
			//we simply omit out-of-range node
			logger.Warningf("Recv out-of-range bucketnode [%+v] (current [%v]", node, offset)
			continue
		}
		bk := newBucketKey(stateImpl.currentConfig, int(offset.Level), int(node.BucketNum))
		stateImpl.bucketTreeDelta.setBucketNode(unmarshalBucketNode(bk, node.CryptoHash))

		logger.Debugf("Recv metadata at bucketKey[%+v]", bk)
	}

	return nil
}
