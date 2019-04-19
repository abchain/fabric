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
	"bytes"
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/util"
	"github.com/abchain/fabric/protos"
	"strconv"
)

// Blockchain holds basic information in memory. Operations on Blockchain are not thread-safe
// TODO synchronize access to in-memory variables
type blockchain struct {
	*db.OpenchainDB
	size                   uint64
	previousBlockHash      []byte
	previousBlockStateHash []byte
	indexer                blockchainIndexer
	lastProcessedBlock     *lastProcessedBlock
}

type lastProcessedBlock struct {
	block       *protos.Block
	blockNumber uint64
	blockHash   []byte
}

var indexBlockDataSynchronously = true

func newBlockchain(db *db.OpenchainDB) (*blockchain, error) {
	size, err := fetchBlockchainSizeFromDB(db)
	if err != nil {
		return nil, err
	}
	blockchain := &blockchain{db, 0, nil, nil, nil, nil}
	blockchain.size = size
	if size > 0 {
		previousBlock, err := fetchBlockFromDB(db, size-1)
		if err != nil {
			return nil, err
		}
		previousBlockHash, err := previousBlock.GetHash()
		if err != nil {
			return nil, err
		}
		blockchain.previousBlockHash = previousBlockHash
		blockchain.previousBlockStateHash = previousBlock.StateHash
	}

	err = blockchain.startIndexer()
	if err != nil {
		return nil, err
	}
	return blockchain, nil
}

func (blockchain *blockchain) startIndexer() (err error) {
	if indexBlockDataSynchronously {
		blockchain.indexer = newBlockchainIndexerSync()
	} else {
		blockchain.indexer = newBlockchainIndexerAsync()
	}
	err = blockchain.indexer.start(blockchain)
	return
}

// getLastBlock get last block in blockchain
func (blockchain *blockchain) getLastBlock() (*protos.Block, error) {
	if blockchain.size == 0 {
		return nil, nil
	}
	return blockchain.getBlock(blockchain.size - 1)
}

// getLastBlock get last block in blockchain
func (blockchain *blockchain) getLastRawBlock() (*protos.Block, error) {
	if blockchain.size == 0 {
		return nil, nil
	}
	return blockchain.getRawBlock(blockchain.size - 1)
}

// getSize number of blocks in blockchain
func (blockchain *blockchain) getSize() uint64 {
	return blockchain.size
}

// getBlock get block at arbitrary height in block chain
func (blockchain *blockchain) getBlock(blockNumber uint64) (*protos.Block, error) {
	return fetchBlockFromDB(blockchain.OpenchainDB, blockNumber)
}

// getBlock get block at arbitrary height in block chain but without transactions,
// this can save many IO but gethash may return wrong value for legacy blocks
func (blockchain *blockchain) getRawBlock(blockNumber uint64) (*protos.Block, error) {
	return fetchRawBlockFromDB(blockchain.OpenchainDB, blockNumber)
}

// getBlockByHash get block by block hash
func (blockchain *blockchain) getBlockByHash(blockHash []byte) (*protos.Block, error) {
	blockNumber, err := blockchain.indexer.fetchBlockNumberByBlockHash(blockHash)
	if err != nil {
		return nil, err
	}
	return blockchain.getBlock(blockNumber)
}

// getBlockByHash get raw block (block without transaction datas) by block hash
func (blockchain *blockchain) getRawBlockByHash(blockHash []byte) (*protos.Block, error) {
	blockNumber, err := blockchain.indexer.fetchBlockNumberByBlockHash(blockHash)
	if err != nil {
		return nil, err
	}
	return blockchain.getRawBlock(blockNumber)
}

func (blockchain *blockchain) getBlockByState(stateHash []byte) (*protos.Block, error) {
	blockNumber, err := blockchain.indexer.fetchBlockNumberByStateHash(stateHash)
	if err != nil {
		return nil, err
	}
	return blockchain.getBlock(blockNumber)
}

// func (blockchain *blockchain) getTransactionByID(txID string) (*protos.Transaction, error) {
// 	blockNumber, txIndex, err := blockchain.indexer.fetchTransactionIndexByID(txID)
// 	if err != nil {
// 		return nil, err
// 	}

// 	return blockchain.getTransaction(blockNumber, txIndex)
// }

// getTransactions get all transactions in a block identified by block number
func (blockchain *blockchain) getTransactions(blockNumber uint64) ([]*protos.Transaction, error) {
	block, err := blockchain.getRawBlock(blockNumber)
	if err != nil {
		return nil, err
	}
	return fetchTxsFromDB(block.GetTxids()), nil
}

// getTransactionsByBlockHash get all transactions in a block identified by block hash
func (blockchain *blockchain) getTransactionsByBlockHash(blockHash []byte) ([]*protos.Transaction, error) {
	block, err := blockchain.getRawBlockByHash(blockHash)
	if err != nil {
		return nil, err
	}
	return fetchTxsFromDB(block.GetTxids()), nil
}

// getTransaction get a transaction identified by block number and index within the block
func (blockchain *blockchain) getTransaction(blockNumber uint64, txIndex uint64) (*protos.Transaction, error) {

	block, err := blockchain.getRawBlock(blockNumber)
	if err != nil {
		return nil, err
	}

	txids := block.GetTxids()
	//out of range
	if uint64(len(txids)) <= txIndex {
		return nil, ErrOutOfBounds
	}

	return fetchTxFromDB(txids[txIndex])
}

// getTransactionByBlockHash get a transaction identified by block hash and index within the block
func (blockchain *blockchain) getTransactionByBlockHash(blockHash []byte, txIndex uint64) (*protos.Transaction, error) {

	block, err := blockchain.getRawBlockByHash(blockHash)
	if err != nil {
		return nil, err
	}

	txids := block.GetTxids()
	//out of range
	if uint64(len(txids)) <= txIndex {
		return nil, ErrOutOfBounds
	}

	return fetchTxFromDB(txids[txIndex])

}

func (blockchain *blockchain) getBlockchainInfo() (*protos.BlockchainInfo, error) {
	if blockchain.getSize() == 0 {
		return &protos.BlockchainInfo{Height: 0}, nil
	}

	lastBlock, err := blockchain.getLastRawBlock()
	if err != nil {
		return nil, err
	}

	info := blockchain.getBlockchainInfoForBlock(blockchain.getSize(), lastBlock)
	return info, nil
}

func (blockchain *blockchain) getBlockchainInfoForBlock(height uint64, block *protos.Block) *protos.BlockchainInfo {

	//when get hash, we must prepare for a incoming "raw" block (hash is not )
	block = compatibleLegacyBlock(block)

	hash, _ := block.GetHash()
	info := &protos.BlockchainInfo{
		Height:            height,
		CurrentBlockHash:  hash,
		PreviousBlockHash: block.PreviousBlockHash}

	return info
}

func (blockchain *blockchain) buildBlock(block *protos.Block, stateHash []byte) *protos.Block {
	block.SetPreviousBlockHash(blockchain.previousBlockHash)
	block.StateHash = stateHash
	if block.NonHashData == nil {
		block.NonHashData = &protos.NonHashData{LocalLedgerCommitTimestamp: util.CreateUtcTimestamp()}
	} else {
		block.NonHashData.LocalLedgerCommitTimestamp = util.CreateUtcTimestamp()
	}
	block.Timestamp = util.CreateUtcTimestamp()
	return block
}

func (blockchain *blockchain) addPersistenceChangesForNewBlock(block *protos.Block, blockNumber uint64, writeBatch *db.DBWriteBatch) error {

	blockHash, err := block.GetHash()
	if err != nil {
		return err
	}

	blockBytes, blockBytesErr := block.GetBlockBytes()
	if blockBytesErr != nil {
		return blockBytesErr
	}
	cf := writeBatch.GetDBHandle().BlockchainCF
	writeBatch.PutCF(cf, encodeBlockNumberDBKey(blockNumber), blockBytes)

	// Need to check as we support out of order blocks in cases such as block/state synchronization. This is
	// real blockchain height, not size.
	if blockchain.getSize() <= blockNumber {
		writeBatch.PutCF(cf, blockCountKey, util.EncodeUint64(blockNumber+1))
	}

	//Notice: in original code (fabric 0.6) only Synchronous index work here
	//but this seems to be extremely weired ...
	//so we make index works after block is succefully persisted
	//if blockchain.indexer.isSynchronous() {
	// blockchain.indexer.createIndexes(block, blockNumber, blockHash, writeBatch)
	//}

	blockchain.lastProcessedBlock = &lastProcessedBlock{block, blockNumber, blockHash}
	return nil
}

func (blockchain *blockchain) blockPersistenceStatus(success bool) error {
	if success {

		if blockchain.lastProcessedBlock == nil {
			panic("No block is added before, code error")
		}

		if blockchain.getSize() <= blockchain.lastProcessedBlock.blockNumber {
			blockchain.size = blockchain.lastProcessedBlock.blockNumber + 1
		}

		blockchain.previousBlockHash = blockchain.lastProcessedBlock.blockHash

		//createIndexs ensure a block become indexed (can be query by index API) after function is called
		err := blockchain.indexer.createIndexes(blockchain.lastProcessedBlock.block,
			blockchain.lastProcessedBlock.blockNumber, blockchain.lastProcessedBlock.blockHash)
		if err != nil {
			return err
		}
		// }
	}
	blockchain.lastProcessedBlock = nil
	return nil
}

// --- Deprecated ----
func (blockchain *blockchain) persistRawBlock(block *protos.Block, blockNumber uint64) error {

	writeBatch := blockchain.NewWriteBatch()
	defer writeBatch.Destroy()

	blockchain.addPersistenceChangesForNewBlock(block, blockNumber, writeBatch)
	err := writeBatch.BatchCommit()
	if err != nil {
		return err
	}

	return blockchain.blockPersistenceStatus(true)
}

//compatibleLegacy switch and following funcs is used for some case when we must
//use a db with legacy bytes of blocks (i.e. in testing)
var compatibleLegacy = false

func compatibleLegacyBlock(block *protos.Block) *protos.Block {

	if !compatibleLegacy {
		return block
	}

	if block.Version == 0 && block.Transactions == nil {
		//CAUTION: this also mutate the input block
		block.Transactions = fetchTxsFromDB(block.Txids)
	}

	return block
}

func finishFetchedBlock(blk *protos.Block) *protos.Block {
	if blk == nil {
		return nil
	}

	if blk.Transactions == nil {
		blk.Transactions = fetchTxsFromDB(blk.Txids)
	} else if blk.Txids == nil {
		//only for compatible with the legacy block bytes
		blk.Txids = make([]string, len(blk.Transactions))
		for i, tx := range blk.Transactions {
			blk.Txids[i] = tx.Txid
		}
	}

	return blk
}

func bytesToBlock(blockBytes []byte) (*protos.Block, error) {

	if len(blockBytes) == 0 {
		return nil, nil
	}
	blk, err := protos.UnmarshallBlock(blockBytes)
	if err != nil {
		return nil, err
	}

	//panic for "legacy" blockbytes, which include transactions data ...
	if blk.Version == 0 && blk.Transactions != nil && !compatibleLegacy {
		panic("DB for blockchain still use legacy bytes, need upgrade first")
	}

	return blk, nil
}

func fetchRawBlockFromDB(odb *db.OpenchainDB, blockNumber uint64) (*protos.Block, error) {

	blockBytes, err := odb.GetFromBlockchainCF(encodeBlockNumberDBKey(blockNumber))
	if err != nil {
		return nil, err
	}

	return bytesToBlock(blockBytes)
}

func fetchRawBlockFromSnapshot(snapshot *db.DBSnapshot, blockNumber uint64) (*protos.Block, error) {

	blockBytes, err := snapshot.GetFromBlockchainCFSnapshot(encodeBlockNumberDBKey(blockNumber))
	if err != nil {
		return nil, err
	}

	return bytesToBlock(blockBytes)
}

func fetchBlockFromDB(odb *db.OpenchainDB, blockNumber uint64) (blk *protos.Block, err error) {

	blk, err = fetchRawBlockFromDB(odb, blockNumber)
	if err != nil {
		return
	}

	blk = finishFetchedBlock(blk)

	return
}

func bytesToBlockNumber(bytes []byte) uint64 {

	if len(bytes) == 0 {
		return 0
	}
	return util.DecodeToUint64(bytes)
}

func fetchBlockchainSizeFromSnapshot(snapshot *db.DBSnapshot) (uint64, error) {

	bytes, err := snapshot.GetFromBlockchainCFSnapshot(blockCountKey)
	if err != nil {
		return 0, err
	}

	return bytesToBlockNumber(bytes), nil
}

func fetchBlockchainSizeFromDB(odb *db.OpenchainDB) (uint64, error) {
	bytes, err := odb.GetFromBlockchainCF(blockCountKey)
	if err != nil {
		return 0, err
	}

	return bytesToBlockNumber(bytes), nil
}

var blockCountKey = []byte("blockCount")

func encodeBlockNumberDBKey(blockNumber uint64) []byte {
	return util.EncodeUint64(blockNumber)
}

func (blockchain *blockchain) String() string {
	var buffer bytes.Buffer
	size := blockchain.getSize()
	for i := uint64(0); i < size; i++ {
		block, blockErr := blockchain.getRawBlock(i)
		if blockErr != nil {
			return ""
		}
		buffer.WriteString("\n----------<block #")
		buffer.WriteString(strconv.FormatUint(i, 10))
		buffer.WriteString(">----------\n")
		buffer.WriteString(block.String())
		buffer.WriteString("\n----------<\\block #")
		buffer.WriteString(strconv.FormatUint(i, 10))
		buffer.WriteString(">----------\n")
	}
	return buffer.String()
}

func (blockchain *blockchain) Dump(level int) {

	size := blockchain.getSize()
	if size == 0 {
		return
	}

	ledgerLogger.Infof("========================blockchain height: %d=============================", size-1)

	for i := uint64(0); i < size; i++ {
		block, blockErr := blockchain.getRawBlock(i)
		if blockErr != nil {
			return
		}
		curBlockHash, _ := block.GetHash()

		ledgerLogger.Infof("high[%s]: \n"+
			"	StateHash<%x>, \n"+
			"	Transactions<%x>, \n"+
			"	curBlockHash<%x> \n"+
			"	prevBlockHash<%x>, \n"+
			"	ConsensusMetadata<%x>, \n"+
			"	timp<%+v>, \n"+
			"	NonHashData<%+v>",
			strconv.FormatUint(i, 10),
			block.StateHash,
			block.Txids,
			curBlockHash,
			block.PreviousBlockHash,
			block.ConsensusMetadata,
			block.Timestamp,
			block.NonHashData)
	}
	ledgerLogger.Info("==========================================================================")
}
