package ledger

import (
	"bytes"
	"fmt"
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/protos"
	"github.com/op/go-logging"
)

//the final implement of indexer, abondon the async design and left commiter for controlling

var indexLogger = logging.MustGetLogger("indexes")

type blockIndexCache struct {
	blockHash, stateHash []byte
	txindex              map[string]int
}

func newBlockIndexCache(blockHash []byte, block *protos.Block) *blockIndexCache {

	idx := &blockIndexCache{
		blockHash: blockHash,
		stateHash: block.GetStateHash(),
		txindex:   make(map[string]int),
	}

	if txids := block.GetTxids(); txids != nil {
		for i, txid := range txids {
			idx.txindex[txid] = i
		}
	} else if txs := block.GetTransactions(); txs != nil {
		indexLogger.Debugf("No txid array found in block, we have deprecated code?")
		for i, tx := range txs {
			idx.txindex[tx.GetTxid()] = i
		}
	}

	return idx
}

func (ind *blockIndexCache) persistence(writeBatch *db.DBWriteBatch, blockNumber uint64) {

	cf := writeBatch.GetCFs().IndexesCF

	indexLogger.Debugf("Indexing block number [%d] by hash = [%x](shash [%12x]", blockNumber, ind.blockHash, ind.stateHash)
	//add the mark
	writeBatch.PutCF(cf, encodeIndexMarkKey(blockNumber), indexMarkingMagicCode)

	writeBatch.PutCF(cf, encodeBlockHashKey(ind.blockHash), encodeBlockNumber(blockNumber))
	if len(ind.stateHash) != 0 {
		writeBatch.PutCF(cf, encodeStateHashKey(ind.stateHash), encodeBlockNumber(blockNumber))
	}

	for id, txind := range ind.txindex {
		writeBatch.PutCF(cf, encodeTxIDKey(id), encodeBlockNumTxIndex(blockNumber, uint64(txind)))
	}

}

type indexerCache map[uint64]*blockIndexCache

func (c indexerCache) fetchTransactionIndex(txID string) (uint64, uint64) {

	for blkI, idx := range c {
		if txI, ok := idx.txindex[txID]; ok {
			return blkI, uint64(txI)
		}
	}

	return 0, 0
}

func (c indexerCache) fetchBlockNumberByBlockHash(hash []byte) uint64 {

	for blkI, idx := range c {
		if bytes.Compare(hash, idx.blockHash) == 0 {
			return blkI
		}
	}

	return 0
}

func (c indexerCache) fetchBlockNumberByStateHash(hash []byte) uint64 {

	for blkI, idx := range c {
		if bytes.Compare(hash, idx.stateHash) == 0 {
			return blkI
		}
	}

	return 0
}

type blockchainIndexerImpl struct {
	*db.OpenchainDB

	cache struct {
		indexedTo indexProgress
		i         indexerCache
		pending   *blockIndexCache
		pendingOn uint64
	}
}

func newIndexer(blockchain *blockchain, rollback int) (*blockchainIndexerImpl, error) {

	odb := blockchain.OpenchainDB
	sizelimit := blockchain.getSize()
	persistedLimit := blockchain.getContinuousBlockHeight()
	if uint64(rollback) > persistedLimit {
		persistedLimit = 0
	} else {
		persistedLimit = sizelimit - uint64(rollback)
	}

	ret := &blockchainIndexerImpl{
		OpenchainDB: odb,
	}

	ret.cache.i = make(map[uint64]*blockIndexCache)
	if sizelimit == 0 {
		return ret, nil
	}

	//restore indexes (which should be persisted)
	if err, doneTo := checkIndexTill(odb, persistedLimit); err != nil {
		return nil, err
	} else if doneTo < persistedLimit {
		return nil, fmt.Errorf("Can not restore index to the specified limit [%d] (get %d)", persistedLimit, doneTo)
	}
	ret.cache.indexedTo.beginBlockID = persistedLimit

	indexLogger.Debugf("scaning data for uncontinuous indexes from %d ... ", persistedLimit)
	scanned := scanAllIndexesForBlock(blockchain.OpenchainDB,
		persistedLimit+1, sizelimit, ret.cache.indexedTo.FinishBlock)

	indexLogger.Infof("init index db done: has indexing %d blocks, continuous to %d, rollback limit %d",
		scanned+persistedLimit+1, ret.cache.indexedTo.GetProgress(), ret.cache.indexedTo.GetTop())

	//now we read the rollback able part into cache, that is, block indexes after persisted
	//and before last block which is continuously persisted...
	if blkCH := blockchain.getContinuousBlockHeight(); blkCH > 0 {
		err, _ := progressIndexs(odb, blkCH-1,
			ret.cache.indexedTo.GetProgress()+1,
			func(blk *protos.Block, num uint64, hash []byte) error {
				ret.cache.i[num] = newBlockIndexCache(hash, blk)
				return nil
			})

		if err != nil {
			return nil, err
		}
	}

	indexLogger.Infof("load index db to cache done: preload %d records", len(ret.cache.i))

	return ret, nil
}

//there seems no preview available for indexer ...
func (indexer *blockchainIndexerImpl) prepareIndexes(block *protos.Block, blockNumber uint64, blockHash []byte) error {

	if indexer.cache.indexedTo.beginBlockID > 0 {
		if blockNumber <= indexer.cache.indexedTo.beginBlockID {
			indexLogger.Warningf("try index persisted block %d (now %d)", blockNumber, indexer.cache.indexedTo.beginBlockID)
		}
	}

	indexer.cache.pending = newBlockIndexCache(blockHash, block)
	indexer.cache.pendingOn = blockNumber
	return nil
}

func (indexer *blockchainIndexerImpl) prepareIndexesFromBlock(block *protos.Block, blockNumber uint64) error {
	blockhash, err := block.GetHash()
	if err != nil {
		return err
	}

	return indexer.prepareIndexes(block, blockNumber, blockhash)
}

func (indexer *blockchainIndexerImpl) commitIndex() {
	indexer.cache.i[indexer.cache.pendingOn] = indexer.cache.pending
}

func (indexer *blockchainIndexerImpl) persistPrepared(writeBatch *db.DBWriteBatch) (uint64, error) {
	return indexer.cache.pendingOn, indexer.persistIndexes(writeBatch, indexer.cache.pendingOn)
}

//like state, indexes can be only persisted in sequences
func (indexer *blockchainIndexerImpl) persistIndexes(writeBatch *db.DBWriteBatch, blockNumber uint64) error {

	//notice the target may has been persisted
	target := indexer.cache.pending
	if blockNumber != indexer.cache.pendingOn {
		target = indexer.cache.i[blockNumber]
	}

	if target != nil {
		target.persistence(writeBatch, blockNumber)
	}

	ciid := indexer.cache.indexedTo.PreviewProgress(blockNumber)
	if ciid > indexer.cache.indexedTo.GetProgress() {
		writeBatch.PutCF(writeBatch.GetCFs().IndexesCF, lastIndexedBlockKey, encodeBlockNumber(ciid))
	}

	return nil
}

//persistent notify
func (indexer *blockchainIndexerImpl) persistDone(blockNumber uint64) {
	delete(indexer.cache.i, blockNumber)
	if indexer.cache.pendingOn == blockNumber {
		indexer.cache.pending = nil
	}

	indexer.cache.indexedTo.FinishBlock(blockNumber)

}

//different from async indexer, we pick db data first
func (indexer *blockchainIndexerImpl) fetchBlockNumberByBlockHash(blockHash []byte) (uint64, error) {

	bi, err := fetchBlockNumberByBlockHashFromDB(indexer.OpenchainDB, blockHash)
	if err == nil {
		return bi, nil
	}

	bi = indexer.cache.i.fetchBlockNumberByBlockHash(blockHash)
	if bi != 0 {
		return bi, nil
	}

	return 0, newLedgerError(ErrorTypeBlockNotFound, fmt.Sprintf("No block indexed with block hash [%x]", blockHash))
}

func (indexer *blockchainIndexerImpl) fetchBlockNumberByStateHash(stateHash []byte) (uint64, error) {

	bi, err := fetchBlockNumberByStateHashFromDB(indexer.OpenchainDB, stateHash)
	if err == nil {
		return bi, nil
	}

	bi = indexer.cache.i.fetchBlockNumberByStateHash(stateHash)
	if bi != 0 {
		return bi, nil
	}

	return 0, newLedgerError(ErrorTypeBlockNotFound, fmt.Sprintf("No block indexed with state hash [%x]", stateHash))
}

func (indexer *blockchainIndexerImpl) fetchTransactionIndexByID(txID string) (bi uint64, ti uint64, err error) {

	bi, ti, err = fetchTransactionIndexByIDFromDB(indexer.OpenchainDB, txID)
	if err == nil {
		return
	}

	bi, ti = indexer.cache.i.fetchTransactionIndex(txID)
	if bi != 0 {
		return
	}
	err = newLedgerError(ErrorTypeBlockNotFound, fmt.Sprintf("No block indexed with transaction id [%s]", txID))
	return
}
