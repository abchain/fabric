package ledger

import (
	"bytes"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/core/ledger/statemgmt/state"
	"github.com/abchain/fabric/protos"
)

//full tx commiter, to build new block and corresponding state on top of current chain
//from the execution results of a bunch of txs. CANNOT concurrent with other commiter
type TxEvaluateAndCommit struct {
	ledger             *Ledger
	accumulatedDeltas  *statemgmt.StateDelta
	transactionResults []*protos.TransactionResult

	lastResult struct {
		position uint64
		block    []byte
		state    []byte
	}
}

func NewTxEvaluatingAgent(ledger *Ledger) (*TxEvaluateAndCommit, error) {

	if ledger.state.isSyncing() {
		return nil, newLedgerError(ErrorTypeInvalidArgument, "state is under syncing")
	}

	return &TxEvaluateAndCommit{
		ledger:            ledger,
		accumulatedDeltas: statemgmt.NewStateDelta(),
	}, nil
}

func (tec *TxEvaluateAndCommit) AssignExecRT() TxExecStates {
	return TxExecStates{state.NewExecStates(append(tec.ledger.state.cache.deltas, tec.accumulatedDeltas)...)}
}

func (tec *TxEvaluateAndCommit) MergeExec(s TxExecStates) {
	tec.accumulatedDeltas.ApplyChanges(s.DeRef())
}

func (tec *TxEvaluateAndCommit) AddExecResult(txRes *protos.TransactionResult) {
	tec.transactionResults = append(tec.transactionResults, txRes)
}

func (tec *TxEvaluateAndCommit) AddExecSuccessResult(s TxExecStates, txRes *protos.TransactionResult) {
	tec.accumulatedDeltas.ApplyChanges(s.DeRef())
	tec.AddExecResult(txRes)
}

func (tec *TxEvaluateAndCommit) Ledger() *Ledger {
	return tec.ledger
}

func (tec *TxEvaluateAndCommit) LastCommitState() []byte    { return tec.lastResult.state }
func (tec *TxEvaluateAndCommit) LastCommitPosition() uint64 { return tec.lastResult.position }

//push state forward one, refblock can be used for verify the expected state
func (tec *TxEvaluateAndCommit) StateCommitOne(refBlockNumber uint64, refBlock *protos.Block) error {
	ledger := tec.ledger
	var err error
	ledgerLogger.Infof("Start state commit txbatch to block %d", refBlockNumber)

	err = ledger.state.prepareState(refBlockNumber, tec.accumulatedDeltas)
	if err != nil {
		return err
	}
	stateHash := ledger.state.getBuildingHash()

	tec.lastResult.position = refBlockNumber
	tec.lastResult.state = stateHash

	if refshash := refBlock.GetStateHash(); refshash != nil {
		if bytes.Compare(stateHash, refshash) != 0 {
			return newLedgerError(ErrorTypeFatalCommit, "local delta not match reference block")
		}
	}

	defer func() {

		ledger.readCache.Lock()
		defer ledger.readCache.Unlock()

		ledger.state.commitState()
		if err == nil {
			ledger.state.persistentStateDone()
			ledger.index.persistDone(refBlockNumber)
		}
	}()

	writeBatch := ledger.blockchain.NewWriteBatch()
	defer writeBatch.Destroy()

	if refBlock.GetNonHashData() == nil {
		buildExecResults(refBlock, tec.transactionResults)
		err = ledger.blockchain.updateBlock(writeBatch, refBlockNumber, refBlock)
		if err != nil {
			return err
		}
	}

	err = ledger.state.persistentState(writeBatch)
	if err != nil {
		return err
	}
	ledger.index.persistIndexes(writeBatch, refBlockNumber)

	err = writeBatch.BatchCommit()
	if err != nil {
		return err
	}

	return nil
}

//export the build block function
var BuildPreviewBlock = buildBlock

//preview build required part of a block for commiting
func (tec *TxEvaluateAndCommit) PreviewBlock(position uint64, blk *protos.Block) (*protos.Block, error) {

	ledger := tec.ledger

	err := ledger.state.prepareState(position, tec.accumulatedDeltas)
	if err != nil {
		return nil, err
	}

	blk.StateHash = ledger.state.getBuildingHash()
	buildExecResults(blk, tec.transactionResults)

	tec.lastResult.position = position
	tec.lastResult.state = blk.StateHash

	err = ledger.blockchain.prepareNewBlock(position, blk, nil)
	if err != nil {
		return nil, err
	}

	return blk, nil
}

//commit current results and persist them
func (tec *TxEvaluateAndCommit) FullCommit(metadata []byte, transactions []*protos.Transaction) error {

	newBlockNumber := tec.ledger.blockchain.getSize()
	ledgerLogger.Infof("Start full commit txbatch to block %d", newBlockNumber)

	blk, err := tec.PreviewBlock(newBlockNumber, buildBlock(metadata, transactions))
	if err != nil {
		return err
	}

	ledger := tec.ledger
	blkInfo := ledger.blockchain.getBuildingBlockchainInfo()

	tec.lastResult.block = blkInfo.GetCurrentBlockHash()

	err = ledger.index.prepareIndexes(blk, newBlockNumber, blkInfo.GetCurrentBlockHash())
	if err != nil {
		return err
	}

	writeBatch := ledger.blockchain.NewWriteBatch()
	defer writeBatch.Destroy()
	err = ledger.index.persistIndexes(writeBatch, newBlockNumber)
	if err != nil {
		return err
	}
	err = ledger.blockchain.persistentBuilding(writeBatch)
	if err != nil {
		return err
	}
	err = ledger.state.persistentState(writeBatch)
	if err != nil {
		return err
	}

	//we lock in-memory variables and db commit process together to make them
	//an atomic change
	err = writeBatch.BatchCommit()
	if err != nil {
		return err
	}

	ledger.readCache.Lock()
	defer ledger.readCache.Unlock()

	//we can put commit and persistent notify together
	ledger.blockchain.commitBuilding()
	ledger.state.commitState()
	ledger.index.commitIndex()

	ledger.blockchain.blockPersisted(newBlockNumber)
	ledger.state.persistentStateDone()
	ledger.index.persistDone(newBlockNumber)

	return nil
}

//commit blocks, can work concurrent with mutiple block commiter, or state commiter
type BlockCommit struct {
	ledger        *Ledger
	lastBlockHash []byte
}

func NewBlockAgent(ledger *Ledger) *BlockCommit {

	return &BlockCommit{ledger, nil}
}

func (blkc BlockCommit) LastCommit() []byte { return blkc.lastBlockHash }

//commit a block in specified position, it make a "full" commit (including persistent
//index and transactions), notice it can not be concurrent with mutiple block commitings becasue
//the db written must be in sequence
func (blkc *BlockCommit) SyncCommitBlock(blkn uint64, block *protos.Block) (err error) {
	ledger := blkc.ledger

	writeBatch := ledger.blockchain.NewWriteBatch()
	defer writeBatch.Destroy()
	defer func() {
		if err == nil {
			err = writeBatch.BatchCommit()
			if err == nil {
				ledger.blockchain.blockPersisted(blkn)
				ledger.index.persistDone(blkn)
			}
		}
	}()

	ledgerLogger.Debugf("Start commit block (prevhash is %x) on %d", block.GetPreviousBlockHash(), blkn)

	ledger.readCache.Lock()
	defer ledger.readCache.Unlock()

	err = ledger.blockchain.prepareBlock(blkn, block)
	if err != nil {
		return
	}

	blkInfo := ledger.blockchain.getBuildingBlockchainInfo()
	defer func(bkhash []byte) {

		if err == nil {
			blkc.lastBlockHash = bkhash
		}

	}(blkInfo.GetCurrentBlockHash())
	err = ledger.index.prepareIndexes(block, blkn, blkInfo.GetCurrentBlockHash())
	if err != nil {
		return
	}

	_, err = ledger.index.persistPrepared(writeBatch)
	if err != nil {
		return err
	}
	err = ledger.blockchain.persistentBuilding(writeBatch)
	if err != nil {
		return err
	}

	ledger.blockchain.commitBuilding()
	ledger.index.commitIndex()

	return ledger.PutTransactions(block.GetTransactions())
}
