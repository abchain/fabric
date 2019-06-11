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

package ledger

import (
	"fmt"

	"encoding/binary"
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
)

var prefixBlockHashKey = byte(1)
var prefixTxIDKey = byte(2)
var prefixAddressBlockNumCompositeKey = byte(3)
var prefixStateHashKey = byte(4)
var prefixIndexMarking = byte(8)
var indexMarkingMagicCode = []byte{42, 42, 42}
var lastIndexedBlockKey = []byte{byte(0)}

type blockchainIndexer interface {
	start(blockchain *blockchain) error
	createIndexes(block *protos.Block, blockNumber uint64, blockHash []byte) error
	fetchBlockNumberByBlockHash(blockHash []byte) (uint64, error)
	fetchBlockNumberByStateHash(stateHash []byte) (uint64, error)
	fetchTransactionIndexByID(txID string) (uint64, uint64, error)
	stop()
}

type indexProgress struct {
	beginBlockID  uint64
	pendingBlocks []bool
}

func (i *indexProgress) GetProgress() uint64 { return i.beginBlockID }
func (i *indexProgress) GetTop() uint64      { return i.beginBlockID + uint64(len(i.pendingBlocks)) }

//this calculate if specified id is add, what the beginBlockID will become
//without change the underlying object, or just beginBlockID for any error
func (i *indexProgress) PreviewProgress(id uint64) uint64 {
	//notice if id is NOT i.beginBlockID+1, this is impossible that it
	//"forwards" beginBlockID
	if id != i.beginBlockID+1 {
		return i.beginBlockID
	}

	if len(i.pendingBlocks) < 1 {
		return id
	}

	for _, t := range i.pendingBlocks[1:] {
		if !t {
			break
		}
		id++
	}

	return id
}

func (i *indexProgress) FinishBlock(id uint64) {

	//0 is not a progress
	if id == 0 {
		return
	}

	if id <= i.beginBlockID {
		indexLogger.Debugf("Have rewinded id [%d] (current %d)", id, i.beginBlockID)
		return
	}

	//laid a shortcut here
	if id == i.beginBlockID+1 && i.pendingBlocks == nil {
		i.beginBlockID = id
		return
	}

	offset := int(id - i.beginBlockID)
	for offset > len(i.pendingBlocks) {
		i.pendingBlocks = append(i.pendingBlocks, false)
	}
	i.pendingBlocks[offset-1] = true

	for ii, done := range i.pendingBlocks {
		if !done {
			i.pendingBlocks = i.pendingBlocks[ii:]
			i.beginBlockID = i.beginBlockID + uint64(ii)
			return
		}
	}

	//if we come here, means all the pendingBlocks has been clean
	i.beginBlockID = i.beginBlockID + uint64(len(i.pendingBlocks))
	i.pendingBlocks = nil
}

// Implementation for sync indexer
type blockchainIndexerSync struct {
	*blockchainIndexerImpl
}

func newBlockchainIndexerSync() *blockchainIndexerSync {
	return &blockchainIndexerSync{}
}

var defaultSyncIndexerRollbackLen = 0

func (indexer *blockchainIndexerSync) start(blockchain *blockchain) error {
	var err error
	indexer.blockchainIndexerImpl, err = newIndexer(blockchain, defaultSyncIndexerRollbackLen)
	if err != nil {
		return err
	}
	return nil
}

func (indexer blockchainIndexerSync) createIndexes(block *protos.Block, blockNumber uint64, blockHash []byte) error {

	if err := indexer.prepareIndexesFromBlock(block, blockNumber); err != nil {
		return err
	}

	indexer.commitIndex()
	if blockNumber < uint64(defaultSyncIndexerRollbackLen) {
		return nil
	}

	persistedBlockNumber := blockNumber - uint64(defaultSyncIndexerRollbackLen)

	writeBatch := indexer.NewWriteBatch()
	defer writeBatch.Destroy()

	if err := indexer.persistIndexes(writeBatch, persistedBlockNumber); err != nil {
		return err
	}

	if err := writeBatch.BatchCommit(); err != nil {
		indexLogger.Errorf("fail in index of block %d: last commit failure [%s]", persistedBlockNumber, err)
		//if failed, prog will become inconsisted with db, but the error of db is fatal enough ....
		return err
	}

	indexer.persistDone(persistedBlockNumber)

	return nil
}

func (indexer blockchainIndexerSync) stop() {
	return
}

func checkIndex(blockchain *blockchain) (error, uint64) {

	totalSize := blockchain.getSize()
	if totalSize == 0 {
		// chain is empty
		return nil, 0
	}

	return checkIndexTill(blockchain.OpenchainDB, totalSize-1)
}

func checkIndexTill(odb *db.OpenchainDB, till uint64) (error, uint64) {

	lastIndexedBlockNum := uint64(0)
	zerothRecord := false
	if lastIndexedBlockNumberBytes, err := odb.GetValue(db.IndexesCF, lastIndexedBlockKey); err != nil {
		return err, 0
	} else if lastIndexedBlockNumberBytes != nil {
		//so if we have a "last indexed" record, we start from which plus one
		lastIndexedBlockNum = decodeBlockNumber(lastIndexedBlockNumberBytes)
	} else {
		zerothRecord = true
	}

	if lastIndexedBlockNum == till {
		// all committed blocks are indexed
		indexLogger.Debugf("Recorded last indexed num has catched up with block (%d)", till)
		return nil, lastIndexedBlockNum
	} else if till < lastIndexedBlockNum {
		indexLogger.Warningf("Encounter a higher index [%d] record than current expected (%d)", lastIndexedBlockNum, till)
		return nil, lastIndexedBlockNum
	}

	indexLogger.Infof("indexer need to finish pending block from %d to %d first, please wait ...", lastIndexedBlockNum, till)

	writeBatch := odb.NewWriteBatch()
	defer writeBatch.Destroy()

	calledCounter := 0
	writef := func(block *protos.Block, blocknum uint64, blockhash []byte) error {
		addIndexDataForPersistence(writeBatch, block, blocknum, blockhash)
		calledCounter++
		//so we commit each 8 blocks
		if calledCounter > 8 {
			calledCounter = 0
			if err := writeBatch.BatchCommit(); err != nil {
				return fmt.Errorf("fail in commit batch: %s", err)
			}
		}
		return nil
	}

	startNum := lastIndexedBlockNum + 1
	if zerothRecord {
		startNum = 0
	}

	err, finalBlockNum := progressIndexs(odb, till, startNum, writef)
	if err == progressNoChange && !zerothRecord {
		indexLogger.Infof("indexer task [%d - %d] finished, no change", lastIndexedBlockNum, till)
		return nil, lastIndexedBlockNum
	} else if err != nil {
		return fmt.Errorf("progressIndexs fail: %s", err), 0
	}

	writeBatch.PutCF(writeBatch.GetDBHandle().IndexesCF, lastIndexedBlockKey, encodeBlockNumber(finalBlockNum))
	if err := writeBatch.BatchCommit(); err != nil {
		return fmt.Errorf("fail in last commit batch: %s", err), 0
	}
	indexLogger.Infof("indexer task [%d - %d] finished at %d", lastIndexedBlockNum, till, finalBlockNum)

	return nil, finalBlockNum
}

var progressNoChange = fmt.Errorf("No change")

//check our progress, obtain the real "lastIndexed" number and return
//startBlockNum is the number checked begins and if this block can not
//be indexed we return "progressNoChange" error
func progressIndexs(odb *db.OpenchainDB, lastCommitedBlockNum uint64, startBlockNum uint64,
	persistIndex func(*protos.Block, uint64, []byte) error) (error, uint64) {

	indexLogger.Debugf("lastCommittedBlockNum=[%d], startBlockNum=[%d]",
		lastCommitedBlockNum, startBlockNum)

	//the cache iterator will greatly help the efficiency
	indMarkItr := odb.GetIterator(db.IndexesCF)
	defer indMarkItr.Close()
	indMarkItr.Seek(encodeIndexMarkKey(startBlockNum))

	for ; startBlockNum <= lastCommitedBlockNum; startBlockNum++ {

		if indMarkItr != nil {
			if !indMarkItr.Valid() || !indMarkItr.ValidForPrefix([]byte{prefixIndexMarking}) {
				indexLogger.Infof("We have no cache from [%d], iterator closed", startBlockNum)
				indMarkItr = nil
			} else if num, err := decodeAndVerifyIndexMarkKey(indMarkItr.Key().Data(), indMarkItr.Value().Data()); err != nil {
				indexLogger.Error("Wrong cache mark data:", err)
				indMarkItr.Next() //TODO: try next? or fail? that's a problem
			} else if num < startBlockNum {
				indexLogger.Errorf("Encounter a less number in key than expected [%d vs %d], something wrong, iterator closed", num, startBlockNum)
				//this should not happen ...
				indMarkItr = nil
			} else if num == startBlockNum {
				//good, we can skip this number and go for next one
				indMarkItr.Next()
				indexLogger.Debugf("Match cached at %d, forward next one", num)
				continue
			} else {
				indexLogger.Debugf("block %d is not marked yet, deep inspect this, next one is %d", startBlockNum, num)
			}
		}

		blockToIndex, err := fetchRawBlockFromDB(odb, startBlockNum)
		//interrupt for any db error
		if err != nil {
			return fmt.Errorf("db fail in getblock %d: %s", startBlockNum, err), startBlockNum
		} else if blockToIndex == nil {
			return fmt.Errorf("db fail in getblock %d: data not exist", startBlockNum), startBlockNum
		}

		//when get hash, we must prepare for a incoming "raw" block (the old version require txs to obtain blockhash)
		blockToIndex = compatibleLegacyBlock(blockToIndex)
		blockHash, err := blockToIndex.GetHash()
		if err != nil {
			return fmt.Errorf("fail in obtained block %d's hash: %s", startBlockNum, err), startBlockNum
		}

		//if we index genesis block, no check is needed
		if startBlockNum != 0 {
			//add checking ...
			if index, err := fetchBlockNumberByBlockHashFromDB(odb, blockHash); err == nil {
				if index == startBlockNum {
					//has been indexed, we can skip
					indexLogger.Infof("Match has been cached at %d, forward next one", startBlockNum)
					//but still add the cache mark so we can go faster even the index building
					//ruined in progress
					odb.PutValue(db.IndexesCF, encodeIndexMarkKey(index), indexMarkingMagicCode)
					continue
				} else if index != 0 {
					indexLogger.Errorf("block %d is indexed at different position %d", startBlockNum, index)
				}
			}
		}

		err = persistIndex(blockToIndex, startBlockNum, blockHash)
		if err != nil {
			return fmt.Errorf("fail in execute persistent index: %s", err), startBlockNum
		}
	}

	//if we come here, startBlockNum must be 1 passed than lastCommitedBlockNum
	return nil, startBlockNum - 1
}

//like scanAllBlocks, but use markers only, so old indexes (without markers)
//can not be recognized
func scanAllIndexesForBlock(odb *db.OpenchainDB, from uint64, till uint64,
	action func(uint64)) uint64 {

	indMarkItr := odb.GetIterator(db.IndexesCF)
	defer indMarkItr.Close()
	total := uint64(0)

	for indMarkItr.Seek(encodeIndexMarkKey(from)); indMarkItr.Valid() && indMarkItr.ValidForPrefix([]byte{prefixIndexMarking}); indMarkItr.Next() {
		height, err := decodeAndVerifyIndexMarkKey(indMarkItr.Key().Data(),
			indMarkItr.Value().Data())
		if err != nil {
			indexLogger.Errorf("Scan for error index marker: %s, omit it", err)
			continue
		} else if height >= till {
			return total
		}
		action(height)
		total++
	}

	return total
}

// -- deprecated -- Functions for persisting and retrieving index data
func addIndexDataForPersistence(writeBatch *db.DBWriteBatch, block *protos.Block, blockNumber uint64, blockHash []byte) {

	tmp := newBlockIndexCache(blockHash, block)
	tmp.persistence(writeBatch, blockNumber)
}

func fetchBlockNumberByBlockHashFromDB(odb *db.OpenchainDB, blockHash []byte) (uint64, error) {
	indexLogger.Debugf("fetchBlockNumberByBlockHashFromDB() for blockhash [%x]", blockHash)
	blockNumberBytes, err := odb.GetValue(db.IndexesCF, encodeBlockHashKey(blockHash))
	if err != nil {
		return 0, err
	}
	indexLogger.Debugf("blockNumberBytes for blockhash [%x] is [%x]", blockHash, blockNumberBytes)
	if len(blockNumberBytes) == 0 {
		return 0, newLedgerError(ErrorTypeBlockNotFound, fmt.Sprintf("No block indexed with block hash [%x]", blockHash))
	}
	blockNumber := decodeBlockNumber(blockNumberBytes)
	return blockNumber, nil
}

func fetchBlockNumberByStateHashFromDB(odb *db.OpenchainDB, stateHash []byte) (uint64, error) {
	indexLogger.Debugf("fetchBlockNumberByStateHashFromDB() for statehash [%x]", stateHash)
	blockNumberBytes, err := odb.GetValue(db.IndexesCF, encodeStateHashKey(stateHash))
	if err != nil {
		return 0, err
	}
	indexLogger.Debugf("blockNumberBytes for statehash [%x] is [%x]", stateHash, blockNumberBytes)
	if len(blockNumberBytes) == 0 {
		return 0, newLedgerError(ErrorTypeBlockNotFound, fmt.Sprintf("No block indexed with block hash [%x]", stateHash))
	}
	blockNumber := decodeBlockNumber(blockNumberBytes)
	return blockNumber, nil
}

func fetchTransactionIndexByIDFromDB(odb *db.OpenchainDB, txID string) (uint64, uint64, error) {
	blockNumTxIndexBytes, err := odb.GetValue(db.IndexesCF, encodeTxIDKey(txID))
	if err != nil {
		return 0, 0, err
	}
	if len(blockNumTxIndexBytes) == 0 {
		return 0, 0, newLedgerError(ErrorTypeBlockNotFound, fmt.Sprintf("No block indexed with transaction id [%s]", txID))
	}
	return decodeBlockNumTxIndex(blockNumTxIndexBytes)
}

func fetchBlockNumberByStateHashFromSnapshot(snapshot *db.DBSnapshot, stateHash []byte) (uint64, error) {
	indexLogger.Debugf("fetchBlockNumberByStateHashFromSnapshot() for statehash [%x]", stateHash)
	blockNumberBytes, err := snapshot.GetFromIndexCFSnapshot(encodeStateHashKey(stateHash))
	if err != nil {
		return 0, err
	}
	indexLogger.Debugf("blockNumberBytes for statehash [%x] is [%x]", stateHash, blockNumberBytes)
	if len(blockNumberBytes) == 0 {
		return 0, newLedgerError(ErrorTypeBlockNotFound, fmt.Sprintf("No block indexed with block hash [%x]", stateHash))
	}
	blockNumber := decodeBlockNumber(blockNumberBytes)
	return blockNumber, nil
}

func getTxExecutingAddress(tx *protos.Transaction) string {
	// TODO Fetch address form tx
	return "address1"
}

func getAuthorisedAddresses(tx *protos.Transaction) ([]string, *protos.ChaincodeID) {
	// TODO fetch address from chaincode deployment tx
	// TODO this method should also return error
	data := tx.ChaincodeID
	cID := &protos.ChaincodeID{}
	err := proto.Unmarshal(data, cID)
	if err != nil {
		return nil, nil
	}
	return []string{"address1", "address2"}, cID
}

// functions for encoding/decoding db keys/values for index data
// encode / decode BlockNumber
func encodeBlockNumber(blockNumber uint64) []byte {
	return proto.EncodeVarint(blockNumber)
}

func decodeBlockNumber(blockNumberBytes []byte) (blockNumber uint64) {
	blockNumber, _ = proto.DecodeVarint(blockNumberBytes)
	return
}

// encode / decode BlockNumTxIndex
func encodeBlockNumTxIndex(blockNumber uint64, txIndexInBlock uint64) []byte {
	b := proto.NewBuffer([]byte{})
	b.EncodeVarint(blockNumber)
	b.EncodeVarint(txIndexInBlock)
	return b.Bytes()
}

func decodeBlockNumTxIndex(bytes []byte) (blockNum uint64, txIndex uint64, err error) {
	b := proto.NewBuffer(bytes)
	blockNum, err = b.DecodeVarint()
	if err != nil {
		return
	}
	txIndex, err = b.DecodeVarint()
	if err != nil {
		return
	}
	return
}

// encode BlockHashKey
func encodeBlockHashKey(blockHash []byte) []byte {
	return prependKeyPrefix(prefixBlockHashKey, blockHash)
}

func encodeStateHashKey(stateHash []byte) []byte {
	return prependKeyPrefix(prefixStateHashKey, stateHash)
}

// encode TxIDKey
func encodeTxIDKey(txID string) []byte {
	return prependKeyPrefix(prefixTxIDKey, []byte(txID))
}

func decodeAndVerifyIndexMarkKey(key []byte, val []byte) (uint64, error) {

	if len(key) < 9 || key[0] != prefixIndexMarking {
		return 0, fmt.Errorf("We obtain wrong key content: %x", key)
	}

	num := binary.BigEndian.Uint64(key[1:])

	//check the magic code ([]byte{42,42,42})
	if len(val) != 3 || val[0] != 42 || val[1] != 42 || val[2] != 42 {
		return num, fmt.Errorf("We obtain a cache mark which contain not the magic code: %x", val)
	}

	return num, nil
}

func encodeIndexMarkKey(num uint64) []byte {

	buffer := [9]byte{prefixIndexMarking}
	binary.BigEndian.PutUint64(buffer[1:], num)
	return buffer[:]
}

func encodeAddressBlockNumCompositeKey(address string, blockNumber uint64) []byte {
	b := proto.NewBuffer([]byte{prefixAddressBlockNumCompositeKey})
	b.EncodeRawBytes([]byte(address))
	b.EncodeVarint(blockNumber)
	return b.Bytes()
}

func encodeListTxIndexes(listTx []uint64) []byte {
	b := proto.NewBuffer([]byte{})
	for i := range listTx {
		b.EncodeVarint(listTx[i])
	}
	return b.Bytes()
}

func prependKeyPrefix(prefix byte, key []byte) []byte {
	modifiedKey := []byte{}
	modifiedKey = append(modifiedKey, prefix)
	modifiedKey = append(modifiedKey, key...)
	return modifiedKey
}
