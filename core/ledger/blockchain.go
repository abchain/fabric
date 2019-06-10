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
	"errors"
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/util"
	"github.com/abchain/fabric/protos"
	"strconv"
	"sync"
)

// Blockchain holds basic information in memory and help
// to build a single block
// ledger will ensure the thread-safty of reading the in-memory variables
type blockchain struct {
	sync.RWMutex
	*db.OpenchainDB

	size                uint64
	continuousTo        indexProgress
	updatesubscriptions []LedgerNewBlockNotify

	//we save all blocks that is not persisted yet
	blockCached struct {
		m map[uint64]*protos.Block
	}
	//it also act as a cache of the "latest" block (when commited is true)
	buildingBlock struct {
		block       *protos.Block
		blockHash   []byte
		blockNumber uint64
		commited    bool
	}

	//-- deprecated --
	indexer     blockchainIndexer
	syncIndexer bool
}

var indexBlockDataSynchronously = true

func newBlockchain2(db *db.OpenchainDB) (*blockchain, error) {
	size, err := fetchBlockchainSizeFromDB(db)
	if err != nil {
		return nil, err
	}

	blockchain := &blockchain{
		OpenchainDB: db,
		size:        size,
		syncIndexer: indexBlockDataSynchronously,
	}

	if size > 0 {
		cblkn, err := fetchContinuousBlockSizeFromDB(db)
		if err != nil {
			return nil, err
		}
		if cblkn == 0 {
			//byte not exist, do a test and restore current continuous blocks
			blockchain.continuousTo.beginBlockID = testBlockExistedRange(db, 0, true) - 1
		} else {
			blockchain.continuousTo.beginBlockID = cblkn
		}

		ledgerLogger.Debugf("scaning data for uncontinuous blocks from %d to %d ... ", cblkn, size)
		scanned := scanAllBlocks(db, cblkn+1, size, func(h uint64, _ []byte) {
			blockchain.continuousTo.FinishBlock(h)
		})
		ledgerLogger.Infof("init blockchain db done: has %d blocks, max block is %d, chain is continuous to %d",
			scanned+cblkn+1, size-1, blockchain.continuousTo.GetProgress())

	} else {
		ledgerLogger.Infof("init blockchain db done, no block")
	}

	return blockchain, nil
}

// --- deprecated ----
func newBlockchain(db *db.OpenchainDB) (*blockchain, error) {

	blockchain, err := newBlockchain2(db)
	if err != nil {
		return nil, err
	}

	if blockchain.syncIndexer {
		blockchain.indexer = newBlockchainIndexerSync()
	} else {
		blockchain.indexer = newBlockchainIndexerAsync()
	}
	err = blockchain.indexer.start(blockchain)
	if err != nil {
		return nil, err
	}
	return blockchain, nil
}

func (blockchain *blockchain) blockIsCached(num uint64) *protos.Block {
	if blockchain.buildingBlock.commited && blockchain.buildingBlock.blockNumber == num {
		return blockchain.buildingBlock.block
	}
	return nil
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

func (blockchain *blockchain) getContinuousBlockHeight() uint64 {
	return blockchain.continuousTo.GetProgress()
}

// getBlock get block at arbitrary height in block chain
func (blockchain *blockchain) getBlock(blockNumber uint64) (*protos.Block, error) {

	if cblk := blockchain.blockIsCached(blockNumber); cblk != nil {
		blockcopy, err := cblk.CloneBlock()
		if err != nil {
			return nil, err
		}
		return finishFetchedBlock(blockcopy)
	}

	return fetchBlockFromDB(blockchain.OpenchainDB, blockNumber)
}

// getBlock get block at arbitrary height in block chain but without transactions,
// this can save many IO but gethash may return wrong value for legacy blocks
func (blockchain *blockchain) getRawBlock(blockNumber uint64) (*protos.Block, error) {

	if cblk := blockchain.blockIsCached(blockNumber); cblk != nil {
		return cblk, nil
	}

	return fetchRawBlockFromDB(blockchain.OpenchainDB, blockNumber)
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

	//when get hash, we must prepare for a incoming "raw" block (hash is not avaliable without txs)
	block = compatibleLegacyBlock(block)

	hash, _ := block.GetHash()
	info := &protos.BlockchainInfo{
		Height:            height,
		CurrentBlockHash:  hash,
		PreviousBlockHash: block.PreviousBlockHash,
		CurrentStateHash:  block.StateHash,
	}

	return info
}

//block-build helper func, without statehash
func buildBlock(metadata []byte, transactions []*protos.Transaction) *protos.Block {
	block := protos.NewBlock(transactions, metadata)
	if block.NonHashData == nil {
		block.NonHashData = &protos.NonHashData{LocalLedgerCommitTimestamp: util.CreateUtcTimestamp()}
	} else {
		block.NonHashData.LocalLedgerCommitTimestamp = util.CreateUtcTimestamp()
	}
	block.Timestamp = util.CreateUtcTimestamp()
	return block
}

//put results into nonhashdata field of block
func buildExecResults(block *protos.Block, transactionResults []*protos.TransactionResult) *protos.Block {
	ccEvents := []*protos.ChaincodeEvent{}
	for _, txr := range transactionResults {

		if txr.ErrorCode != 0 {
			//build an error event, (chaincodeID is omitted)
			errEvt := &protos.ChaincodeEvent{
				EventName: protos.EventName_TxError,
				TxID:      txr.GetTxid(),
			}
			//use the payload to carry both error string and code
			wrapRes := &protos.TransactionResult{
				Error:     txr.GetError(),
				ErrorCode: txr.GetErrorCode(),
			}
			errEvt.Payload, _ = wrapRes.Bytes()
			ccEvents = append(ccEvents, errEvt)
		} else {
			//need to filter out the nil/empty event passed in legacy code :(
			for _, evts := range txr.ChaincodeEvents {
				if evts.GetChaincodeID() != "" {
					ccEvents = append(ccEvents, evts)
				}

			}
		}
	}
	block.NonHashData = &protos.NonHashData{ChaincodeEvents: ccEvents}
	return block
}

//preview
func (blockchain *blockchain) getBuildingBlockchainInfo() *protos.BlockchainInfo {

	return &protos.BlockchainInfo{
		Height:            blockchain.buildingBlock.blockNumber + 1,
		CurrentBlockHash:  blockchain.buildingBlock.blockHash,
		PreviousBlockHash: blockchain.buildingBlock.block.GetPreviousBlockHash(),
		CurrentStateHash:  blockchain.buildingBlock.block.GetStateHash(),
	}
}

//notice, preview info is not buildinginfo, this entry get the "real" chain info
//after current building block is persisted (not just commited), it also return
//the continuous block height in the secondary return value
func (blockchain *blockchain) previewBlockchainInfo() (*protos.BlockchainInfo, uint64) {
	previewedCid := blockchain.continuousTo.PreviewProgress(blockchain.buildingBlock.blockNumber)
	if blockchain.buildingBlock.blockNumber >= blockchain.size {
		return blockchain.getBuildingBlockchainInfo(), previewedCid
	} else {
		info, _ := blockchain.getBlockchainInfo()
		return info, previewedCid
	}
}

//add an block to building cache, notice if you DO wish to specify a empty previousBlockHash,
//use []byte{} instead of nil, for nil, module will try to search and add an previousblockhash
//for current building block, and throw error when it can not do that
func (blockchain *blockchain) prepareNewBlock(blkNumber uint64, block *protos.Block, previousBlockHash []byte) error {

	if previousBlockHash == nil && blkNumber > 0 {
		//try to find the previous block hash
		prevBlk, err := blockchain.getRawBlock(blkNumber - 1)
		if err != nil {
			return err
		} else if prevH, err := prevBlk.GetHash(); err != nil {
			return err
		} else {
			previousBlockHash = prevH
		}

	}
	block.PreviousBlockHash = previousBlockHash

	return blockchain.prepareBlock(blkNumber, block)
}

//add a block completely specified
func (blockchain *blockchain) prepareBlock(blkNumber uint64, block *protos.Block) error {
	var err error
	blockchain.buildingBlock.blockHash, err = block.GetHash()
	if err != nil {
		return err
	}

	blockchain.buildingBlock.commited = false
	blockchain.buildingBlock.block = block
	blockchain.buildingBlock.blockNumber = blkNumber
	return nil
}

//help to update the non-hash part of a block
func (blockchain *blockchain) updateBlock(writeBatch *db.DBWriteBatch, blkNumber uint64, block *protos.Block) error {

	blockBytes, err := blockchain.buildingBlock.block.GetBlockBytes()
	if err != nil {
		return err
	}

	cf := writeBatch.GetDBHandle().BlockchainCF
	writeBatch.PutCF(cf, encodeBlockNumberDBKey(blkNumber), blockBytes)
	return nil
}

func (blockchain *blockchain) persistentBuilding(writeBatch *db.DBWriteBatch) error {

	blockBytes, err := blockchain.buildingBlock.block.GetBlockBytes()
	if err != nil {
		return err
	}

	cf := writeBatch.GetDBHandle().BlockchainCF
	writeBatch.PutCF(cf, encodeBlockNumberDBKey(blockchain.buildingBlock.blockNumber), blockBytes)

	nextCid := blockchain.continuousTo.PreviewProgress(blockchain.buildingBlock.blockNumber)
	if nextCid > blockchain.continuousTo.GetProgress() {
		writeBatch.PutCF(cf, continuousblockCountKey, util.EncodeUint64(nextCid))
	}
	//caution: we keep writting the best value of current block size: because persistentBuilding
	//may be put in any sequence with commit (i.e. blockchain.size may has been updated or not)
	if blockchain.buildingBlock.blockNumber >= blockchain.size {
		writeBatch.PutCF(cf, blockCountKey, util.EncodeUint64(blockchain.buildingBlock.blockNumber+1))
	} else {
		writeBatch.PutCF(cf, blockCountKey, util.EncodeUint64(blockchain.size))
	}

	return nil
}

func (blockchain *blockchain) commitBuilding() {
	if blockchain.buildingBlock.commited {
		panic("wrong code: try to double commit block")
	}

	if blockchain.size <= blockchain.buildingBlock.blockNumber {
		blockchain.size = blockchain.buildingBlock.blockNumber + 1
	}
	blockchain.buildingBlock.commited = true
}

func (blockchain *blockchain) blockPersisted(blkn uint64) {
	blockchain.continuousTo.FinishBlock(blkn)
	theblk, _ := blockchain.getRawBlock(blkn)
	for _, f := range blockchain.updatesubscriptions {
		f(blkn, theblk)
	}
}

// --- Deprecated following ----

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

func (blockchain *blockchain) buildBlock(block *protos.Block, stateHash []byte) *protos.Block {
	block.SetPreviousBlockHash(blockchain.buildingBlock.blockHash)
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

	err := blockchain.prepareBlock(blockNumber, block)
	if err != nil {
		return err
	}
	blockchain.commitBuilding()
	blockchain.persistentBuilding(writeBatch)
	return nil
}

func (blockchain *blockchain) blockPersistenceStatus(success bool) error {
	if success {

		//createIndexs ensure a block become indexed (can be query by index API) after function is called
		//(all commit task has done, we could not reverse them even index is fail)
		blockchain.indexer.createIndexes(blockchain.buildingBlock.block,
			blockchain.buildingBlock.blockNumber, blockchain.buildingBlock.blockHash)

		blockchain.blockPersisted(blockchain.buildingBlock.blockNumber)
		return nil
	}
	return nil
}

// --- Deprecated above ----

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

func finishFetchedBlock(blk *protos.Block) (*protos.Block, error) {
	if blk == nil {
		return nil, errors.New("block is nil")
	}

	if blk.Transactions == nil {
		blk.Transactions = fetchTxsFromDB(blk.Txids)
		if len(blk.Transactions) < len(blk.Txids) {
			return nil, errors.New("transactions can not be obtained")
		}

	} else if blk.Txids == nil {
		//only for compatible with the legacy block bytes
		blk.Txids = make([]string, len(blk.Transactions))
		for i, tx := range blk.Transactions {
			blk.Txids[i] = tx.Txid
		}
	}

	return blk, nil
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
	} else if blk == nil {
		return nil, nil
	}

	return finishFetchedBlock(blk)

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

func fetchContinuousBlockSizeFromDB(odb *db.OpenchainDB) (uint64, error) {
	bytes, err := odb.GetFromBlockchainCF(continuousblockCountKey)
	if err != nil {
		return 0, err
	}

	return bytesToBlockNumber(bytes), nil
}

//simply test if block has been saved for the specified height
func testBlockExisted(odb *db.OpenchainDB, height uint64) bool {

	blockBytes, _ := odb.GetFromBlockchainCF(encodeBlockNumberDBKey(height))
	return len(blockBytes) > 0
}

//the legacy version of testBlockExistedRangeSafe
func testBlockExistedRange(odb *db.OpenchainDB, height uint64, upside bool) uint64 {
	return testBlockExistedRangeSafe(odb, height, 0, upside)
}

//obtain the highest/lowest block number adjacent to
//a continuous range, which include the specified height
//if it return height itself, indicating block at height is not existed
//function also return when it hit limit
func testBlockExistedRangeSafe(odb *db.OpenchainDB, height uint64, limit uint64, upside bool) uint64 {

	if upside && height == 0 {
		//we omit 0 for upside test: it was always supposed to be existed
		height = 1
	}

	blockItr := odb.GetIterator(db.BlockchainCF)
	defer blockItr.Close()

	var nextF, nextHeight func()
	if upside {
		nextF = blockItr.Next
		nextHeight = func() { height++ }
	} else {
		nextF = blockItr.Prev
		nextHeight = func() { height-- }
	}

	for blockItr.Seek(encodeBlockNumberDBKey(height)); height > 0 && height != limit && blockItr.Valid(); nextF() {

		//we had to filter out the two "marking" key (luckily)...
		if len(blockItr.Key().Data()) > 8 {
			continue
		}

		if height != decodeBlockNumberDBKey(blockItr.Key().Data()) {
			break
		}
		nextHeight()
	}

	return height
}

//scan all blocks, and send each data of block into action func,
//notice action must do COPY the block bytes for using later
//it scan blocks in the range of [from, to)
func scanAllBlocks(odb *db.OpenchainDB, from uint64, till uint64, action func(uint64, []byte)) uint64 {

	blockItr := odb.GetIterator(db.BlockchainCF)
	defer blockItr.Close()

	total := uint64(0)
	for blockItr.Seek(encodeBlockNumberDBKey(from)); blockItr.Valid(); blockItr.Next() {

		height := decodeBlockNumberDBKey(blockItr.Key().Data())
		if height >= till {
			return total
		}
		action(height, blockItr.Value().Data())
		total++
	}

	return total
}

var continuousblockCountKey = []byte("conblockCount")
var blockCountKey = []byte("blockCount")

var encodeBlockNumberDBKey = util.EncodeUint64
var decodeBlockNumberDBKey = util.DecodeToUint64

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
