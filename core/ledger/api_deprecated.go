package ledger

import (
	"fmt"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/core/ledger/statemgmt/state"
	"github.com/abchain/fabric/protos"
	"reflect"
)

// BlockChainAccessor interface for retreiving blocks by block number
type BlockChainAccessor interface {
	GetBlockByNumber(blockNumber uint64) (*protos.Block, error)
	GetBlockchainSize() uint64
	GetCurrentStateHash() (stateHash []byte, err error)
}

// BlockChainModifier interface for applying changes to the block chain
type BlockChainModifier interface {
	ApplyStateDelta(id interface{}, delta *statemgmt.StateDelta) error
	RollbackStateDelta(id interface{}) error
	CommitStateDelta(id interface{}) error
	EmptyState() error
	PutBlock(blockNumber uint64, block *protos.Block) error
}

// BlockChainUtil interface for interrogating the block chain
type BlockChainUtil interface {
	HashBlock(block *protos.Block) ([]byte, error)
	VerifyBlockchain(start, finish uint64) (uint64, error)
}

// StateAccessor interface for query states of current history and global
type StateAccessor interface {
	GetStateSnapshot() (*state.StateSnapshot, error)
	GetStateDelta(blockNumber uint64) (*statemgmt.StateDelta, error)
	GetGlobalState(statehash []byte) *protos.GlobalState
}

// StateManager interface for manage history view and global states
type StateManager interface {
	AddGlobalState(parent []byte, state []byte) error
}

// all the depecrated implement is thrown here ...
// YA-fabric 0.9: Notice all the state related API in ledger has been deprecated,
// Use TxExecState object instead

func UseLegacyModeOnLedger() {}

// BeginTxBatch - gets invoked when next round of transaction-batch execution begins
func (ledger *Ledger) BeginTxBatch(id interface{}) error {
	err := ledger.checkValidIDBegin()
	if err != nil {
		return err
	}

	ledger.currentStateHash, err = ledger.state.GetHash()
	if err != nil {
		return err
	}

	ledger.currentID = id
	return nil
}

func (ledger *Ledger) checkValidIDBegin() error {
	if ledger.currentID != nil {
		return fmt.Errorf("Another TxGroup [%s] already in-progress", ledger.currentID)
	}
	return nil
}

func (ledger *Ledger) checkValidIDCommitORRollback(id interface{}) error {
	if !reflect.DeepEqual(ledger.currentID, id) {
		return fmt.Errorf("Another TxGroup [%v] already in-progress", ledger.currentID)
	}
	return nil
}

func (ledger *Ledger) resetForNextTxGroup(txCommited bool) {
	ledgerLogger.Debug("resetting ledger state for next transaction batch")
	ledger.currentID = nil
	ledger.state.ClearInMemoryChanges(txCommited)
}

// CommitTxBatch - gets invoked when the current transaction-batch needs to be committed
// This function returns successfully iff the transactions details and state changes (that
// may have happened during execution of this transaction-batch) have been committed to permanent storage
func (ledger *Ledger) CommitTxBatch(id interface{}, transactions []*protos.Transaction, transactionResults []*protos.TransactionResult, metadata []byte) error {
	err := ledger.checkValidIDCommitORRollback(id)
	if err != nil {
		return err
	}

	ledger.txpool.poolTransaction(transactions)
	hashBefore := ledger.state.getHash()

	tmpCommiter := &TxEvaluateAndCommit{ledger: ledger}
	tmpCommiter.accumulatedDeltas = ledger.state.GetStateDelta()
	//ledgerLogger.Debugf("use commit delta %v", tmpCommiter.accumulatedDeltas)
	tmpCommiter.transactionResults = transactionResults
	tmpCommiter.transactions = transactions

	//simply force the state to change its height, made some test pass
	curHeight := ledger.blockchain.size
	curSHeight := ledger.state.cache.curHeight
	if curSHeight < curHeight {
		ledger.state.updatePersistentStateTo(curHeight)
	}

	defer ledger.resetForNextTxGroup(false)
	if err := tmpCommiter.FullCommit(metadata); err != nil {
		ledgerLogger.Error("Full CommitTx fail:", err)
		return err
	}

	info := ledger.blockchain.getBuildingBlockchainInfo()
	block, _ := ledger.blockchain.getLastRawBlock()
	//commit tx, them add globalstate
	//the built block contain all txids
	if dbErr := ledger.txpool.commitTransaction(block.GetTxids(), info.Height-1); dbErr != nil {
		ledgerLogger.Error("CommitTx fail:", dbErr)
	}

	if dbErr := ledger.AddGlobalState(hashBefore, ledger.state.getHash()); dbErr != nil {
		ledgerLogger.Error("at add global state phase fail:", dbErr)
	}

	sendProducerBlockEvent(block.GetPruneDeployment())
	//send chaincode events from transaction results
	errTxs := sendChaincodeEvents(transactionResults)
	if errTxs != 0 {
		ledgerLogger.Debugf("Handling %d Txs, %d is error", len(transactions), errTxs)
	}

	ledgerLogger.Debugf("Committed: [%v], blockchain size(heigth): %d",
		info,
		ledger.GetBlockchainSize())

	return nil
}

// RollbackTxBatch - Discards all the state changes that may have taken place during the execution of
// current transaction-batch
func (ledger *Ledger) RollbackTxBatch(id interface{}) error {
	ledgerLogger.Debugf("RollbackTxBatch for id = [%s]", id)
	err := ledger.checkValidIDCommitORRollback(id)
	if err != nil {
		return err
	}
	ledger.resetForNextTxGroup(false)
	return nil
}

// TxBegin - Marks the begin of a new transaction in the ongoing batch
func (ledger *Ledger) TxBegin(txID string) {
	ledger.state.TxBegin(txID)
}

// TxFinished - Marks the finish of the on-going transaction.
// If txSuccessful is false, the state changes made by the transaction are discarded
func (ledger *Ledger) TxFinished(txID string, txSuccessful bool) {
	ledger.state.TxFinish(txID, txSuccessful)
}

// GetState get state for chaincodeID and key. If committed is false, this first looks in memory
// and if missing, pulls from db.  If committed is true, this pulls from the db only.
func (ledger *Ledger) GetState(chaincodeID string, key string, committed bool) ([]byte, error) {
	if committed {
		return ledger.state.Get(chaincodeID, key, nil, 0)
	} else {
		return ledger.state.GetTransient(chaincodeID, key)
	}
}

func (ledger *Ledger) GetSnapshotState(chaincodeID string, key string, blknum uint64) ([]byte, uint64, error) {

	ledger.readCache.RLock()
	//if blknum is just the top block, we can get current state,
	if bcSize := ledger.blockchain.getSize(); blknum+1 == bcSize {
		v, err := ledger.GetState(chaincodeID, key, true)
		ledger.readCache.RUnlock()
		return v, blknum, err
	}
	ledger.readCache.RUnlock()

	sn, actualblk := ledger.snapshots.GetSnapshot(blknum)
	if sn == nil {
		return nil, 0, ErrOutOfBounds
	}
	defer sn.Release()
	//sanity check
	if blknum < actualblk {
		panic("Unexpect history offset")
	}

	v, err := ledger.state.Get(chaincodeID, key, sn, int(blknum-actualblk))
	return v, actualblk, err
}

// GetStateRangeScanIterator returns an iterator to get all the keys (and values) between startKey and endKey
// (assuming lexical order of the keys) for a chaincodeID.
// If committed is true, the key-values are retrieved only from the db. If committed is false, the results from db
// are mergerd with the results in memory (giving preference to in-memory data)
// The key-values in the returned iterator are not guaranteed to be in any specific order
func (ledger *Ledger) GetStateRangeScanIterator(chaincodeID string, startKey string, endKey string, committed bool) (statemgmt.RangeScanIterator, error) {
	return ledger.state.GetRangeScanIterator(chaincodeID, startKey, endKey, committed)
}

// SetState sets state to given value for chaincodeID and key. Does not immideatly writes to DB
func (ledger *Ledger) SetState(chaincodeID string, key string, value []byte) error {
	if key == "" || value == nil {
		return newLedgerError(ErrorTypeInvalidArgument,
			fmt.Sprintf("An empty string key or a nil value is not supported. Method invoked with key='%s', value='%#v'", key, value))
	}
	return ledger.state.Set(chaincodeID, key, value)
}

// DeleteState tracks the deletion of state for chaincodeID and key. Does not immediately writes to DB
func (ledger *Ledger) DeleteState(chaincodeID string, key string) error {
	return ledger.state.Delete(chaincodeID, key)
}

// CopyState copies all the key-values from sourceChaincodeID to destChaincodeID
func (ledger *Ledger) CopyState(sourceChaincodeID string, destChaincodeID string) error {
	return ledger.state.CopyState(sourceChaincodeID, destChaincodeID)
}

// GetStateMultipleKeys returns the values for the multiple keys.
// This method is mainly to amortize the cost of grpc communication between chaincode shim peer
// YA-fabric: deprecated this method for we have more sophisticated entries for getting (transient/snapshot)
// func (ledger *Ledger) GetStateMultipleKeys(chaincodeID string, keys []string, committed bool) ([][]byte, error) {
// 	return ledger.state.GetMultipleKeys(chaincodeID, keys, committed)
// }

// SetStateMultipleKeys sets the values for the multiple keys.
// This method is mainly to amortize the cost of grpc communication between chaincode shim peer
func (ledger *Ledger) SetStateMultipleKeys(chaincodeID string, kvs map[string][]byte) error {
	return ledger.state.SetMultipleKeys(chaincodeID, kvs)
}

/////////////////// world-state related methods /////////////////////////////////////
/////////////////////////////////////////////////////////////////////////////////////

// GetTempStateHash - Computes state hash by taking into account the state changes that may have taken
// place during the execution of current transaction-batch
func (ledger *Ledger) GetTempStateHash() ([]byte, error) {
	return ledger.state.GetHash()
}

// GetCurrentStateHash returns the current non-committed hash of the in memory state
func (ledger *Ledger) GetCurrentStateHash() (stateHash []byte, err error) {

	ledger.readCache.RLock()
	defer ledger.readCache.RUnlock()

	if ledger.state.isSyncing() {
		return nil, newLedgerError(ErrorTypeResourceNotFound, "state not ready")
	}
	return ledger.state.getBuildingHash(), nil
}

// ApplyStateDelta applies a state delta to the current state. This is an
// in memory change only. You must call ledger.CommitStateDelta to persist
// the change to the DB.
// This should only be used as part of state synchronization. State deltas
// can be retrieved from another peer though the Ledger.GetStateDelta function
// or by creating state deltas with keys retrieved from
// Ledger.GetStateSnapshot(). For an example, see TestSetRawState in
// ledger_test.go
// Note that there is no order checking in this function and it is up to
// the caller to ensure that deltas are applied in the correct order.
// For example, if you are currently at block 8 and call this function
// with a delta retrieved from Ledger.GetStateDelta(10), you would now
// be in a bad state because you did not apply the delta for block 9.
// It's possible to roll the state forwards or backwards using
// stateDelta.RollBackwards. By default, a delta retrieved for block 3 can
// be used to roll forwards from state at block 2 to state at block 3. If
// stateDelta.RollBackwards=false, the delta retrieved for block 3 can be
// used to roll backwards from the state at block 3 to the state at block 2.
func (ledger *Ledger) ApplyStateDelta(id interface{}, delta *statemgmt.StateDelta) error {
	err := ledger.checkValidIDBegin()
	if err != nil {
		return err
	}
	ledger.currentID = id
	ledger.state.ApplyStateDelta(delta)
	return nil
}

func (ledger *Ledger) ApplyStateDeltaDirect(delta *statemgmt.StateDelta) {
	ledger.state.MergeStateDelta(delta)
}

// ----- Will be deprecated ----
// CommitStateDelta will commit the state delta passed to ledger.ApplyStateDelta
// to the DB
func (ledger *Ledger) CommitStateDelta(id interface{}) error {
	err := ledger.checkValidIDCommitORRollback(id)
	if err != nil {
		return err
	}
	defer ledger.resetForNextTxGroup(true)

	err = ledger.state.CommitStateDelta()
	if err != nil {
		return err
	}

	return nil
}

// CommitStateDelta will commit the state delta passed to ledger.ApplyStateDelta
// to the DB and persist it will the specified index (blockNumber)
func (ledger *Ledger) CommitAndIndexStateDelta(id interface{}, blockNumber uint64) error {

	err := ledger.checkValidIDCommitORRollback(id)
	if err != nil {
		return err
	}
	stateHash, err := ledger.state.GetHash()
	if err != nil {
		return err
	}

	defer ledger.resetForNextTxGroup(true)

	writeBatch := ledger.state.NewWriteBatch()
	defer writeBatch.Destroy()
	ledger.state.AddChangesForPersistence(blockNumber, writeBatch)

	err = writeBatch.BatchCommit()
	if err != nil {
		return err
	}

	ledger.snapshots.Update(stateHash, blockNumber)
	return nil
}

// RollbackStateDelta will discard the state delta passed
// to ledger.ApplyStateDelta
func (ledger *Ledger) RollbackStateDelta(id interface{}) error {
	err := ledger.checkValidIDCommitORRollback(id)
	if err != nil {
		return err
	}
	ledger.resetForNextTxGroup(false)
	return nil
}

// PutBlock put a raw block on the chain and also commit the corresponding transactions
func (ledger *Ledger) PutBlock(blockNumber uint64, block *protos.Block) error {

	commitids := block.GetTxids()
	if len(commitids) == 0 {
		for _, tx := range block.GetTransactions() {
			commitids = append(commitids, tx.GetTxid())
		}
	}

	//for old styple block, we prepare for that the tx may not be pooled yet
	if len(block.GetTransactions()) > 0 {
		ledger.txpool.poolTransaction(block.Transactions)
	}

	if err := ledger.PutRawBlock(block, blockNumber); err != nil {
		return err
	}

	//commiting txs MUST run AFTER the corresponding block is persisted
	return ledger.txpool.commitTransaction(commitids, blockNumber)
}

// PutRawBlock just puts a raw block on the chain without handling the tx
func (ledger *Ledger) PutRawBlock(block *protos.Block, blockNumber uint64) error {

	tmpAgent := NewBlockAgent(ledger)
	err := tmpAgent.SyncCommitBlock(blockNumber, block)
	if err != nil {
		return err
	}

	sendProducerBlockEvent(block)
	return nil
}
