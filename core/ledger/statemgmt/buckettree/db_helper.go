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
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger/statemgmt"
)

// fetch one DataNode FromDB by a dataKey
func fetchDataNodeFromDB(odb *db.OpenchainDB, dataKey *dataKey) (*dataNode, error) {
	nodeBytes, err := odb.GetValue(db.StateCF, dataKey.getEncodedBytes())
	if err != nil {
		return nil, err
	}
	if nodeBytes == nil {
		logger.Debug("nodeBytes from db is nil")
	} else if len(nodeBytes) == 0 {
		logger.Debug("nodeBytes from db is an empty array")
	}
	// key does not exist
	if nodeBytes == nil {
		return nil, nil
	}
	return unmarshalDataNode(dataKey, nodeBytes), nil
}

// fetch one DataNode FromDB by a dataKey
func fetchDataNodeFromSnapshot(sn *db.DBSnapshot, dataKey *dataKey) (*dataNode, error) {
	nodeBytes, err := sn.GetFromStateCFSnapshot(dataKey.getEncodedBytes())
	if err != nil {
		return nil, err
	}
	if nodeBytes == nil {
		logger.Debug("nodeBytes from db is nil")
	} else if len(nodeBytes) == 0 {
		logger.Debug("nodeBytes from db is an empty array")
	}
	// key does not exist
	if nodeBytes == nil {
		return nil, nil
	}
	return unmarshalDataNode(dataKey, nodeBytes), nil
}

func fetchBucketNodeFromDB(odb *db.OpenchainDB, bucketKey *bucketKey) (*bucketNode, error) {
	nodeBytes, err := odb.GetValue(db.StateCF, bucketKey.getEncodedBytes())
	if err != nil {
		return nil, err
	}
	if nodeBytes == nil {
		return nil, nil
	}
	return unmarshalBucketNode(bucketKey, nodeBytes), nil
}

type rawKey []byte

func fetchDataNodesFromDBFor(odb *db.OpenchainDB, bucketKey *bucketKey) (dataNodes, error) {

	itr := odb.GetIterator(db.StateCF)
	defer itr.Close()
	minimumDataKeyBytes := minimumPossibleDataKeyBytesFor(bucketKey)

	logger.Infof("===============>>>> Fetching from DB data nodes for bucket [%s], minimumDataKeyBytes<%x>",
		bucketKey, minimumDataKeyBytes)

	var dataNodes dataNodes
	itr.Seek(minimumDataKeyBytes)

	for ; itr.Valid(); itr.Next() {

		// making a copy of key-value bytes because, underlying key bytes are reused by itr.
		// no need to free slices as iterator frees memory when closed.
		keyBytes := statemgmt.Copy(itr.Key().Data())
		valueBytes := statemgmt.Copy(itr.Value().Data())

		logger.Infof("newDataKeyFromEncodedBytes<%x><%s>", keyBytes, string(valueBytes))

		dataKey := newDataKeyFromEncodedBytes(keyBytes)
		logger.Infof("Retrieved data key [%s] from DB for bucket [%s]", dataKey, bucketKey)
		if !bucketKey.bucketKeyLite.equals(dataKey.getBucketKey(bucketKey.config)) {
			logger.Debugf("Data key [%s] from DB does not belong to bucket = [%s]. Stopping further iteration and returning results [%v]", dataKey, bucketKey, dataNodes)
			return dataNodes, nil
		}
		dataNode := unmarshalDataNode(dataKey, valueBytes)

		logger.Infof("Data node [%s] from DB belongs to bucket = [%s]. Including the key in results...", dataNode, bucketKey)
		dataNodes = append(dataNodes, dataNode)
	}
	logger.Debugf("Returning results [%v]", dataNodes)
	logger.Debugf("[%s], <%d> data nodes: [%v]", bucketKey, len(dataNodes), dataNodes)
	return dataNodes, nil
}

func fetchBucketNode(snapshotHandler *db.DBSnapshot, odb *db.OpenchainDB, bucketKey *bucketKey) (*bucketNode, error) {

	var nodeBytes []byte
	var err error

	if snapshotHandler != nil {
		nodeBytes, err = snapshotHandler.GetFromStateCFSnapshot(bucketKey.getEncodedBytes())
	} else {
		return fetchBucketNodeFromDB(odb, bucketKey)
	}

	if err != nil {
		return nil, err
	}
	if nodeBytes == nil {
		return nil, nil
	}
	return unmarshalBucketNode(bucketKey, nodeBytes), nil
}

//func produceStateDeltaFromDB(start, end int, itr stateCfIterator) (*statemgmt.StateDelta, error) {
//
//	var dataNodes dataNodes = nil
//	for i := start; i <= end; i++ {
//
//		detal, err := fetchDataNodesFromDBFor(itr, &bucketKey{conf.lowestLevel, i})
//
//		if err != nil {
//			return nil, err
//		}
//
//		if dataNodes == nil {
//			dataNodes = detal
//		} else {
//			dataNodes = append(dataNodes, detal...)
//		}
//	}
//
//	stateDelta := statemgmt.NewStateDelta()
//	for _, dataNode := range dataNodes {
//		ccdId, key := dataNode.getKeyElements()
//
//		logger.Infof("<%s> stateDelta.Set: [%s][%s]: %s", dataNode.dataKey,
//			ccdId, key, dataNode.getValue())
//		stateDelta.Set(ccdId, key, dataNode.getValue(), nil)
//	}
//
//	return stateDelta, nil
//}

//func DumpDataNodes() (dataNodes, error) {
//	itr := db.GetDBHandle().GetIterator(db.StateCF)
//	defer itr.Close()
//	minimumDataKeyBytes := minimumPossibleDataKeyBytesFor(newBucketKeyAtLowestLevel(1))
//
//	logger.Infof("minimumDataKeyBytes [%x]", minimumDataKeyBytes)
//
//	var dataNodes dataNodes
//	itr.Seek(minimumDataKeyBytes)
//
//	idx := 1
//	for ; itr.Valid(); itr.Next() {
//		// making a copy of key-value bytes because, underlying key bytes are reused by itr.
//		// no need to free slices as iterator frees memory when closed.
//		keyBytes := statemgmt.Copy(itr.Key().Data())
//		valueBytes := statemgmt.Copy(itr.Value().Data())
//
//		dataKey := newDataKeyFromEncodedBytes(keyBytes)
//		dataNode := unmarshalDataNode(dataKey, valueBytes)
//
//		logger.Infof("Data node[%d]: [%s]", idx, dataNode)
//		idx++
//		dataNodes = append(dataNodes, dataNode)
//	}
//	logger.Debugf("Returning results [%v]", dataNodes)
//	return dataNodes, nil
//}

// return root hash of a bucket tree consisted of all dataNodes belong to bucket nodes between [lv-0, lv-bucketNum] include,
// if lv is the lowest level, then the bucket tree contains all all dataNode [0, bucketNum]
//func ComputeStateHashByOffset(offset *pb.SyncOffset, snapshotHandler *db.DBSnapshot) ([]byte, error) {
//
//	btoffset, err := offset.Unmarshal2BucketTree()
//
//	if err != nil {
//		return nil, err
//	}
//
//	lv, bucketNum := int(btoffset.Level), int(btoffset.BucketNum+btoffset.Delta-1)
//	necessaryBuckets := conf.getNecessaryBuckets(lv, bucketNum)
//	bucketTree := newBucketTreeDelta()
//	for _, bucketKey := range necessaryBuckets {
//		bucketNode, err := fetchBucketNode(snapshotHandler, db.GetDBHandle(), bucketKey)
//
//		if err != nil {
//			logger.Errorf("Failed to fetch BucketNode<%s> From Snapshot, err: %s\n", bucketKey, err)
//			return nil, err
//		}
//
//		bucketTree.getOrCreateBucketNode(bucketKey)
//		if bucketNode != nil && bucketNode.childrenCryptoHash != nil {
//			logger.Debugf("<%s>: %d children\n", bucketKey, len(bucketNode.childrenCryptoHash))
//			bucketTree.byLevel[bucketKey.level][bucketKey.bucketNumber] = bucketNode
//		} else {
//			logger.Debugf("<%s>: 0 children\n", bucketKey)
//		}
//	}
//
//	for level := conf.lowestLevel; level > 0; level-- {
//		bucketNodes := bucketTree.getBucketNodesAt(level)
//
//		for _, node := range bucketNodes {
//
//			logger.Debugf("node.bucketKey<%s>", node.bucketKey)
//
//			parentKey := node.bucketKey.getParentKey()
//			cryptoHash := node.computeCryptoHash()
//
//			if level == conf.lowestLevel {
//				index := parentKey.getChildIndex(node.bucketKey)
//				parentBucketNodeOnDisk, err := fetchBucketNode(snapshotHandler, db.GetDBHandle(), parentKey)
//
//				if err != nil {
//					logger.Errorf("<%s>, index<%d> in parent child num: <%d>, err: %s\n",
//						node.bucketKey, index, err)
//					return nil, err
//				}
//
//				if parentBucketNodeOnDisk == nil {
//					logger.Debugf("<%s>, index<%d>, no parentKey<%s> ondisk\n", node.bucketKey, index, parentKey)
//				} else {
//					cryptoHash = parentBucketNodeOnDisk.childrenCryptoHash[index]
//					logger.Debugf("<%s>, index<%d> in parent child num: <%d>\n", node.bucketKey,
//						index, len(parentBucketNodeOnDisk.childrenCryptoHash))
//				}
//			}
//
//			bucketNodeInMem := bucketTree.getOrCreateBucketNode(parentKey)
//			bucketNodeInMem.setChildCryptoHash(node.bucketKey, cryptoHash)
//			logger.Debugf("set <%s> hash into parent <%s>, hash<%x>\n",
//				node.bucketKey, parentKey, cryptoHash)
//		}
//	}
//
//	hash := bucketTree.getRootNode().computeCryptoHash()
//	logger.Infof("<level-bucketNum><%d-%d>: root hash <%x>\n", lv, bucketNum, hash)
//	return hash, nil
//}
