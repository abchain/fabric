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
	"fmt"
	"sync"

	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/events/producer"
	"github.com/op/go-logging"

	"github.com/abchain/fabric/protos"
)

var ledgerLogger = logging.MustGetLogger("ledger")

//ErrorType represents the type of a ledger error
type ErrorType string

const (
	//ErrorTypeInvalidArgument used to indicate the invalid input to ledger method
	ErrorTypeInvalidArgument = ErrorType("InvalidArgument")
	//ErrorTypeOutOfBounds used to indicate that a request is out of bounds
	ErrorTypeOutOfBounds = ErrorType("OutOfBounds")
	//ErrorTypeResourceNotFound used to indicate if a resource is not found
	ErrorTypeResourceNotFound = ErrorType("ResourceNotFound")
	//ErrorTypeBlockNotFound used to indicate if a block is not found when looked up by it's hash
	ErrorTypeBlockNotFound = ErrorType("ErrorTypeBlockNotFound")
)

//Error can be used for throwing an error from ledger code.
type Error struct {
	errType ErrorType
	msg     string
}

func (ledgerError *Error) Error() string {
	return fmt.Sprintf("LedgerError - %s: %s", ledgerError.errType, ledgerError.msg)
}

//Type returns the type of the error
func (ledgerError *Error) Type() ErrorType {
	return ledgerError.errType
}

func newLedgerError(errType ErrorType, msg string) *Error {
	return &Error{errType, msg}
}

var (
	ErrNormalEnd = newLedgerError(ErrorType("None"), "ledger: normal end")

	// ErrOutOfBounds is returned if a request is out of bounds
	ErrOutOfBounds = newLedgerError(ErrorTypeOutOfBounds, "ledger: out of bounds")

	// ErrResourceNotFound is returned if a resource is not found
	ErrResourceNotFound = newLedgerError(ErrorTypeResourceNotFound, "ledger: resource not found")
)

// Ledger - the struct for openchain ledger
// Threadsafy: we purpose ledger is a read-safe object for multi-thread but
// NOT write-safe
type Ledger struct {
	*LedgerGlobal
	blockchain       *blockchain
	index            *blockchainIndexerImpl
	state            *stateWrapper
	snapshots        *ledgerHistory
	currentID        interface{}
	currentStateHash []byte
	dbVer            int
	//cache MUST be clean if the underlying
	//db has been switched
	//readCache should also guard some fields in blockchain/state object
	//like blockchain.getSize()
	readCache struct {
		sync.RWMutex
	}
}

var ledger *Ledger
var ledgerError error
var once sync.Once

func SetDefaultLedger(l *Ledger) {
	once.Do(func() { ledger = l })
}

// GetLedger - gives a reference to a 'singleton' ledger
func GetLedger() (*Ledger, error) {
	once.Do(func() {
		//check version before obtain ledger ...
		ledgerError = UpgradeLedger(db.GetDBHandle(), true)
		if ledgerError == nil {
			ledger, ledgerError = GetNewLedger(db.GetDBHandle(), nil)
		}
	})
	return ledger, ledgerError
}

// GetNewLedger - gives a reference to a new ledger TODO need better approach
func GetNewLedger(db *db.OpenchainDB, config *ledgerConfig) (*Ledger, error) {

	gledger, err := GetLedgerGlobal()
	if err != nil {
		return nil, err
	}

	blockchain, err := newBlockchain2(db)
	if err != nil {
		return nil, err
	}

	indexer, err := newIndexer(blockchain, defaultSnapshotInterval)
	if err != nil {
		return nil, err
	}

	st, err := newStateWrapper(indexer, config)
	if err != nil {
		return nil, err
	}

	ver := db.GetDBVersion()
	if ver >= 1 {
		err = sanityCheck(db)
		if err != nil {
			return nil, err
		}
	}

	sns := initNewLedgerSnapshotManager(db, st, config)

	return &Ledger{
		LedgerGlobal: gledger,
		blockchain:   blockchain,
		index:        indexer,
		state:        st,
		snapshots:    sns,
		dbVer:        ver}, nil
}

//we need this check and correction for handling the inconsistent between two db (global
//and state), because we need to update both of them when commiting Txs while
//inter-dbs transaction can't not be applied
//
func sanityCheck(odb *db.OpenchainDB) error {

	size, err := fetchBlockchainSizeFromDB(odb)
	if err != nil {
		return fmt.Errorf("Fetch size fail: %s", err)
	}

	if size == 0 {
		return nil
	}

	ledgerLogger.Debug("Start a sanitycheck")

	noneExistingList := make([][]byte, 0)
	var lastExisting []byte

	for n := size; n != 0; n-- {

		block, err := fetchRawBlockFromDB(odb, n-1)
		if err != nil {
			return fmt.Errorf("Fetch block fail: %s", err)
		}
		if block == nil {
			return fmt.Errorf("Block %d is not exist yet", n-1)
		}

		gs := db.GetGlobalDBHandle().GetGlobalState(block.StateHash)
		if gs != nil {
			lastExisting = block.StateHash
			ledgerLogger.Infof("Block number [%d] state exists in txdb, statehash: [%x]",
				n-1, block.StateHash)
			break
		} else {
			ledgerLogger.Warningf("Block number [%d] state does not exist in txdb, statehash: [%x]",
				n-1, block.StateHash)
			noneExistingList = append(noneExistingList, block.StateHash)
		}
	}

	//**** we NEVER restruct the root of global states (it can be only updated in upgrade phase)
	/*
		if lastExisting == nil {
			block, err := fetchRawBlockFromSnapshot(snapshot, n)
			if err != nil {
				return fmt.Errorf("Fetch gensis block fail: %s", err)
			}
			err = db.GetGlobalDBHandle().PutGenesisGlobalState(block.StateHash)
			if err != nil {
				return fmt.Errorf("Put genesis state fail: %s", err)
			}
		}
	*/
	if len(noneExistingList) > 0 && lastExisting == nil {
		return fmt.Errorf("The whole blockchain is not match with global state")
	}

	//so we have all blocks missed in global state (except for the gensis)
	//so we just start from a new gensis state
	for len(noneExistingList) > 0 {

		pos := len(noneExistingList) - 1
		stateHash := noneExistingList[pos]
		err = db.GetGlobalDBHandle().AddGlobalState(lastExisting, stateHash)
		if err != nil {
			return fmt.Errorf("Put global state fail: %s", err)
		}
		lastExisting = stateHash
		noneExistingList = noneExistingList[:pos]
		ledgerLogger.Infof("Sanity check add Missed GlobalState [%x]:", stateHash)
	}

	return nil
}

//overload AddGlobalState in ledgerGlobal
func (ledger *Ledger) AddGlobalState(parent []byte, state []byte) error {

	if ledger.dbVer < 1 {
		ledgerLogger.Debugf("Omit global state for dbversion 0")
		return nil
	}

	if ret := ledger.LedgerGlobal.AddGlobalState(parent, state); ret == nil {
		return nil
	} else {
		//TODO: we should not need this sanitycheck here?
		if _, ok := ret.(parentNotExistError); ok {
			err := sanityCheck(ledger.blockchain.OpenchainDB)
			if err != nil {
				return err
			}
		} else {
			return ret
		}
	}

	return ledger.LedgerGlobal.AddGlobalState(parent, state)
}

/////////////////// Transaction-batch related methods ///////////////////////////////
/////////////////////////////////////////////////////////////////////////////////////

// GetTXBatchPreviewBlockInfo returns a preview block info that will
// contain the same information as GetBlockchainInfo will return after
// ledger.CommitTxBatch is called with the same parameters. If the
// state is modified by a transaction between these two calls, the
// contained hash will be different.
func (ledger *Ledger) GetTXBatchPreviewBlockInfo(id interface{},
	transactions []*protos.Transaction, metadata []byte) (*protos.BlockchainInfo, error) {
	err := ledger.checkValidIDCommitORRollback(id)
	if err != nil {
		return nil, err
	}
	stateHash, err := ledger.state.GetHash()
	if err != nil {
		return nil, err
	}
	block := ledger.blockchain.buildBlock(protos.NewBlock(transactions, metadata), stateHash)
	info := ledger.blockchain.getBlockchainInfoForBlock(ledger.blockchain.getSize()+1, block)
	return info, nil
}

// GetTempStateHashWithTxDeltaStateHashes - In addition to the state hash (as defined in method GetTempStateHash),
// this method returns a map [txUuid of Tx --> cryptoHash(stateChangesMadeByTx)]
// Only successful txs appear in this map
func (ledger *Ledger) GetTempStateHashWithTxDeltaStateHashes() ([]byte, map[string][]byte, error) {
	stateHash, err := ledger.state.GetHash()
	return stateHash, ledger.state.GetTxStateDeltaHash(), err
}

// GetStateDelta will return the state delta for the specified block if
// available.  If not available because it has been discarded, returns nil,nil.
func (ledger *Ledger) GetStateDelta(blockNumber uint64) (*statemgmt.StateDelta, error) {
	if blockNumber >= ledger.GetBlockchainSize() {
		return nil, ErrOutOfBounds
	}
	return ledger.state.FetchStateDeltaFromDB(blockNumber)
}

// alias of DeleteALLStateKeysAndValues
func (ledger *Ledger) EmptyState() error {
	return ledger.DeleteALLStateKeysAndValues()
}

// DeleteALLStateKeysAndValues deletes all keys and values from the state.
// This is generally only used during state synchronization when creating a
// new state from a snapshot.
func (ledger *Ledger) DeleteALLStateKeysAndValues() error {
	return ledger.state.DeleteState()
}

/////////////////// transaction related methods /////////////////////////////////////
/////////////////////////////////////////////////////////////////////////////////////

func (ledger *Ledger) GetCommitedTransaction(txID string) (*protos.Transaction, error) {
	ledger.readCache.RLock()
	defer ledger.readCache.RUnlock()

	//we need to check if this tx is really come into block by query it in blockchain-index
	bln, _, err := ledger.index.fetchTransactionIndexByID(txID)
	if err != nil {
		return nil, err
	} else if bln == 0 {
		//not indexed tx is considered as not commited (even it was in db)
		return nil, nil
	}

	return fetchTxFromDB(txID)
}

// GetTransactionByID return transaction by it's txId
func (ledger *Ledger) GetTransactionsByRange(statehash []byte, beg int, end int) ([]*protos.Transaction, error) {

	ledger.readCache.RLock()
	defer ledger.readCache.RUnlock()

	blockNumber, err := ledger.index.fetchBlockNumberByStateHash(statehash)
	if err != nil {
		return nil, err
	}

	blk, err := ledger.blockchain.getRawBlock(blockNumber)
	if err != nil {
		return nil, err
	}

	//double verify
	if bytes.Compare(blk.GetStateHash(), statehash) != 0 {
		return nil, newLedgerError(ErrorTypeBlockNotFound, "indexed block not match")
	}

	if blk.GetTxids() == nil || len(blk.Txids) < end {
		return nil, ErrOutOfBounds
	}

	return fetchTxsFromDB(blk.Txids[beg:end]), nil
}

/////////////////// blockchain related methods /////////////////////////////////////
/////////////////////////////////////////////////////////////////////////////////////

// GetBlockchainInfo returns information about the blockchain ledger such as
// height, current block hash, and previous block hash.
func (ledger *Ledger) GetBlockchainInfo() (*protos.BlockchainInfo, error) {

	ledger.readCache.RLock()
	defer ledger.readCache.RUnlock()
	return ledger.blockchain.getBlockchainInfo()
}

// HashBlock returns the hash of the included block, useful for mocking
func (*Ledger) HashBlock(block *protos.Block) ([]byte, error) {
	return block.GetHash()
}

func (ledger *Ledger) TestExistedBlockRange(blockNumber uint64) uint64 {
	ledger.readCache.RLock()
	defer ledger.readCache.RUnlock()
	limit := ledger.blockchain.getContinuousBlockHeight()
	ret := testBlockExistedRangeSafe(ledger.blockchain.OpenchainDB, blockNumber, limit, false)
	if ret == limit {
		return 0
	}
	return ret
}

//test how height the blocks we have obtained continuously
func (ledger *Ledger) TestContinuouslBlockRange() (ret uint64) {

	ledger.readCache.RLock()
	defer ledger.readCache.RUnlock()
	return ledger.blockchain.getContinuousBlockHeight()
}

// GetBlockByNumber return block given the number of the block on blockchain.
// Lowest block on chain is block number zero
func (ledger *Ledger) GetBlockByNumber(blockNumber uint64) (*protos.Block, error) {
	ledger.readCache.RLock()
	defer ledger.readCache.RUnlock()
	if blockNumber >= ledger.GetBlockchainSize() {
		return nil, ErrOutOfBounds
	}
	return ledger.blockchain.getBlock(blockNumber)
}

func (ledger *Ledger) GetRawBlockByNumber(blockNumber uint64) (*protos.Block, error) {
	ledger.readCache.RLock()
	defer ledger.readCache.RUnlock()
	if blockNumber >= ledger.GetBlockchainSize() {
		return nil, ErrOutOfBounds
	}
	return ledger.blockchain.getRawBlock(blockNumber)
}

// GetBlockNumberByState return given block nuber from a state, also used to check
// states
func (ledger *Ledger) GetBlockNumberByState(hash []byte) (uint64, error) {

	ledger.readCache.RLock()
	defer ledger.readCache.RUnlock()

	return ledger.index.fetchBlockNumberByStateHash(hash)
}

func (ledger *Ledger) GetBlockNumberByTxid(txID string) (uint64, uint64, error) {
	//TODO: cache?
	if tx := ledger.txpool.getPooledTx(txID); tx != nil {
		return 0, 0, ErrResourceNotFound
	}

	ledger.readCache.RLock()
	defer ledger.readCache.RUnlock()
	return ledger.index.fetchTransactionIndexByID(txID)
}

// GetBlockchainSize returns number of blocks in blockchain
func (ledger *Ledger) GetBlockchainSize() uint64 {
	ledger.readCache.RLock()
	defer ledger.readCache.RUnlock()
	return ledger.blockchain.getSize()
}

// VerifyChain will verify the integrity of the blockchain. This is accomplished
// by ensuring that the previous block hash stored in each block matches
// the actual hash of the previous block in the chain. The return value is the
// block number of lowest block in the range which can be verified as valid.
// The first block is assumed to be valid, and an error is only returned if the
// first block does not exist, or some other sort of irrecoverable ledger error
// such as the first block failing to hash is encountered.
// For example, if VerifyChain(0, 99) is called and previous hash values stored
// in blocks 8, 32, and 42 do not match the actual hashes of respective previous
// block 42 would be the return value from this function.
// highBlock is the high block in the chain to include in verification. If you
// wish to verify the entire chain, use ledger.GetBlockchainSize() - 1.
// lowBlock is the low block in the chain to include in verification. If
// you wish to verify the entire chain, use 0 for the genesis block.
func (ledger *Ledger) VerifyChain(highBlock, lowBlock uint64) (uint64, error) {
	if highBlock >= ledger.GetBlockchainSize() {
		return highBlock, ErrOutOfBounds
	}
	if highBlock < lowBlock {
		return lowBlock, ErrOutOfBounds
	}

	currentBlock, err := ledger.GetBlockByNumber(highBlock)
	if err != nil {
		return highBlock, fmt.Errorf("Error fetching block %d.", highBlock)
	}
	if currentBlock == nil {
		return highBlock, fmt.Errorf("Block %d is nil.", highBlock)
	}

	for i := highBlock; i > lowBlock; i-- {
		previousBlock, err := ledger.GetBlockByNumber(i - 1)
		if err != nil {
			return i, nil
		}
		if previousBlock == nil {
			return i, nil
		}
		previousBlockHash, err := previousBlock.GetHash()
		if err != nil {
			return i, nil
		}
		if bytes.Compare(previousBlockHash, currentBlock.PreviousBlockHash) != 0 {
			return i, nil
		}
		currentBlock = previousBlock
	}

	return lowBlock, nil
}

//alias of verifychain
func (ledger *Ledger) VerifyBlockchain(highBlock, lowBlock uint64) (uint64, error) {
	return ledger.VerifyChain(highBlock, lowBlock)
}

func sendProducerBlockEvent(block *protos.Block) {

	producer.Send(producer.CreateBlockEvent(block))
}

//send chaincode events created by transactions, and stastic the error count
func sendChaincodeEvents(trs []*protos.TransactionResult) (errcnt int) {

	if trs != nil {
		for _, tr := range trs {
			if tr.ErrorCode != 0 {
				errcnt++
				producer.Send(producer.CreateRejectionEvent2(tr.GetTxid(), tr.GetError()))
			} else {
				for _, evt := range tr.ChaincodeEvents {
					if evt.GetChaincodeID() != "" {
						producer.Send(producer.CreateChaincodeEvent(evt))
					}
				}
			}
		}
	}

	return
}
