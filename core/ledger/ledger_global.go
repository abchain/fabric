package ledger

import (
	"fmt"
	"sync"

	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
)

/*
	** YA-fabric 0.9 **
	We have divided the ledger into two parts: the "global" part which just wrapped the
	"globalDB" is kept being a singleton while the "sole" part which include a
	standalone db object within it. the later is made as the legacy ledger struct which
	containing a "global" ledger object to be compatible with the legacy codes
*/

// Ledger - the struct for openchain ledger
type LedgerGlobal struct {
	txpool *transactionPool
}

var ledger_g *LedgerGlobal
var ledger_gError error
var ledger_gOnce sync.Once

// GetLedger - gives a reference to a 'singleton' global ledger, it was the only singleton
// part (the ledger singleton is just for compatible)
func GetLedgerGlobal() (*LedgerGlobal, error) {
	ledger_gOnce.Do(func() {
		if ledger_gError == nil {
			txpool, err := newTxPool()
			if err != nil {
				ledger_gError = err
				return
			}
			ledger_g = &LedgerGlobal{txpool}
		}
	})
	return ledger_g, ledger_gError
}

/////////////////// global state related methods /////////////////////////////////////
func (ledger *LedgerGlobal) GetGlobalState(statehash []byte) *protos.GlobalState {
	return db.GetGlobalDBHandle().GetGlobalState(statehash)
}

func (ledger *LedgerGlobal) GetConsensusData(statehash []byte) []byte {
	val, err := db.GetGlobalDBHandle().GetValue(db.ConsensusCF, statehash)
	if err != nil {
		ledgerLogger.Errorf("Get consensus from db fail: %s", err)
		return nil
	}
	return val
}

type parentNotExistError struct {
	state []byte
}

func (e parentNotExistError) Error() string {
	return fmt.Sprintf("Try to add state in unexist global state [%x]", e.state)
}

func (ledger *LedgerGlobal) AddGlobalState(parent []byte, state []byte) error {

	s := db.GetGlobalDBHandle().GetGlobalState(parent)

	if s == nil {
		return parentNotExistError{parent}
	}

	err := db.GetGlobalDBHandle().AddGlobalState(parent, state)

	if err != nil {
		//should this the correct way to omit StateDuplicatedError?
		if _, ok := err.(db.StateDuplicatedError); !ok {
			ledgerLogger.Errorf("Add globalstate fail: %s", err)
			return err
		}

		ledgerLogger.Warningf("Try to add existed globalstate: %x", state)
	}

	ledgerLogger.Infof("Add globalstate [%x] on parent [%x]", state, parent)
	return nil
}

/////////////////// transaction related methods /////////////////////////////////////

func (ledger *LedgerGlobal) AddCommitHook(hf func([]string, uint64)) {
	ledger.txpool.commitHooks = append(ledger.txpool.commitHooks, hf)
}

func (ledger *LedgerGlobal) PruneTransactions(txs []*protos.Transaction) {
	ledger.txpool.cleanTransaction(txs)
}

func (ledger *LedgerGlobal) PoolTransactions(txs []*protos.Transaction) {
	ledger.txpool.poolTransaction(txs)
}

func (ledger *LedgerGlobal) IteratePooledTransactions(ctx context.Context) (chan *protos.Transaction, error) {
	return ledger.txpool.iteratePooledTx(ctx)
}

func (ledger *LedgerGlobal) PutTransactions(txs []*protos.Transaction) error {
	return ledger.txpool.putTransaction(txs)
}

// GetTransactionByID return transaction by it's txId
func (ledger *LedgerGlobal) GetTransactionByID(txID string) (*protos.Transaction, error) {
	return ledger.txpool.getTransaction(txID)
}

func (ledger *LedgerGlobal) GetPooledTransaction(txID string) *protos.Transaction {
	return ledger.txpool.getPooledTx(txID)
}

func (ledger *LedgerGlobal) GetPooledTxCount() int {
	return ledger.txpool.getPooledTxCount()
}

//we have a mroe sophisticated way to obtain a bunch of transactions
func (ledger *LedgerGlobal) GetTransactionsByID(txIDs []string) []*protos.Transaction {
	txs := ledger.txpool.getPooledTxs(txIDs)

	var cnt int
	var err error
	for i, ret := range txs {
		if ret == nil {
			ret, err = fetchTxFromDB(txIDs[i])
			if err != nil {
				ledgerLogger.Errorf("Fail to obtain tx from db: %s, give up", err)
				break
			}
		}
		if ret != nil {
			txs[cnt] = ret
			cnt++
		}
	}

	return txs[:cnt]
}
