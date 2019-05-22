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

func (tec *TxEvaluateAndCommit) AddExecResult(s TxExecStates, txRes *protos.TransactionResult) {
	tec.accumulatedDeltas.ApplyChanges(s.DeRef())
	tec.transactionResults = append(tec.transactionResults)
}

func (tec *TxEvaluateAndCommit) Ledger() *Ledger {
	return tec.ledger
}

//push state forward one
func (tec *TxEvaluateAndCommit) StateCommitOne(refBlockNumber uint64, refBlock *protos.Block) error {
	ledger := tec.ledger
	var err error
	ledgerLogger.Infof("Start state commit txbatch to block %d", refBlockNumber)

	if refBlock == nil {
		refBlock, err = ledger.blockchain.getRawBlock(refBlockNumber)
		if err != nil {
			return err
		}
	}

	err = ledger.state.prepareState(refBlockNumber, tec.accumulatedDeltas)
	if err != nil {
		return err
	}
	stateHash := ledger.state.getBuildingHash()

	if bytes.Compare(stateHash, refBlock.GetStateHash()) != 0 {
		return newLedgerError(ErrorTypeFatalCommit, "local delta not match reference block")
	}

	writeBatch := ledger.blockchain.NewWriteBatch()
	defer writeBatch.Destroy()

	err = ledger.state.persistentState(writeBatch)
	if err != nil {
		return err
	}

	ledger.readCache.Lock()
	defer ledger.readCache.Unlock()

	ledger.state.commitState()
	ledger.state.persistentStateDone()

	return nil
}

//commit current results and persist them
func (tec *TxEvaluateAndCommit) FullCommit(metadata []byte, transactions []*protos.Transaction) error {

	ledger := tec.ledger

	newBlockNumber := ledger.blockchain.getSize()
	ledgerLogger.Infof("Start full commit txbatch to block %d", newBlockNumber)

	err := ledger.state.prepareState(newBlockNumber, tec.accumulatedDeltas)
	if err != nil {
		return err
	}
	stateHash := ledger.state.getBuildingHash()

	block := buildBlock(stateHash, metadata, transactions)
	buildExecResults(block, tec.transactionResults)
	err = ledger.blockchain.prepareNewBlock(newBlockNumber, block, nil)
	if err != nil {
		return err
	}

	blkInfo := ledger.blockchain.getBuildingBlockchainInfo()
	err = ledger.index.prepareIndexes(block, newBlockNumber, blkInfo.GetCurrentBlockHash())
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
	ledger *Ledger
}

func NewBlockAgent(ledger *Ledger) BlockCommit {

	return BlockCommit{ledger}
}

//commit a block in specified position, it make a "full" commit (including persistent
//and index), notice it can not be concurrent with mutiple block commitings becasue
//the db written must be in sequence
func (blkc BlockCommit) SyncCommitBlock(blkn uint64, block *protos.Block) (err error) {
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

	return nil
}
