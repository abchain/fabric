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

	err := db.GetGlobalDBHandle().AddGlobalState(parent, state)

	if err != nil {
		//should this the correct way to omit StateDuplicatedError?
		if _, ok := err.(db.StateDuplicatedError); !ok {
			ledgerLogger.Errorf("Add globalstate fail: %s", err)
			return err
		}

		ledgerLogger.Warningf("Try to add existed globalstate: %.16X", state)
	}

	ledgerLogger.Infof("Add globalstate [%.12X] on parent [%.12X]", state, parent)
	return nil
}

/////////////////// transaction related methods /////////////////////////////////////

func (ledger *LedgerGlobal) AddCommitHook(hf func([]string, uint64)) {
	ledger.txpool.commitHooks = append(ledger.txpool.commitHooks, hf)
}

func (ledger *LedgerGlobal) PruneTransactions(txs []*protos.Transaction) {
	ledger.txpool.cleanTransaction(txs)
}

func (ledger *LedgerGlobal) PoolTransaction(txe *protos.TransactionHandlingContext) {
	ledger.txpool.poolTxe(txe)
}

func (ledger *LedgerGlobal) IteratePooledTransactions(ctx context.Context) (chan *protos.TransactionHandlingContext, error) {
	return ledger.txpool.iteratePooledTx(ctx)
}

func (ledger *LedgerGlobal) GetPooledTxCount() int {
	return ledger.txpool.getPooledTxCount()
}

func (ledger *LedgerGlobal) PutTransactions(txs []*protos.Transaction) error {
	return putTxsToDB(txs)
}

// GetTransactionByID return transaction by it's txId
func (ledger *LedgerGlobal) GetTransactionByID(txID string) (*protos.Transaction, error) {
	txe := ledger.txpool.getPooledTx(txID)
	if txe == nil {

		tx, err := fetchTxFromDB(txID)
		if err != nil {
			return nil, err
		}

		if tx != nil {
			return tx, nil
		}

		return nil, ErrResourceNotFound
	}

	return txe.Transaction, nil
}

func (ledger *LedgerGlobal) MustGetTransactionByID(txID string) *protos.Transaction {
	tx, err := ledger.GetTransactionByID(txID)
	if err != nil {
		return nil
	}
	return tx
}

//return tx arrays, has same lens of the incoming txIDs, for any id unavaliable, nil
//is returned
func (ledger *LedgerGlobal) GetTxForExecution(txIDs []string, preh protos.TxPreHandler) ([]*protos.TransactionHandlingContext, []string) {

	txes := ledger.txpool.getPooledTxs(txIDs)
	var leftTxs []string

	for i, ret := range txes {
		if ret == nil {
			tx, err := fetchTxFromDB(txIDs[i])
			if err != nil {
				ledgerLogger.Errorf("Fail to obtain tx from db: %s, give up", err)
				leftTxs = append(leftTxs, txIDs[i])
			} else if tx != nil {
				txes[i], err = preh.Handle(protos.NewTransactionHandlingContext(tx))
			}

			if tx == nil || err != nil {
				leftTxs = append(leftTxs, txIDs[i])
			}
		}
	}

	return txes, leftTxs
}

func (ledger *LedgerGlobal) GetTransactionsByIDAligned(txIDs []string) []*protos.Transaction {

	txs := make([]*protos.Transaction, len(txIDs))

	for i, ret := range ledger.txpool.getPooledTxs(txIDs) {
		if ret == nil {
			txs[i], _ = fetchTxFromDB(txIDs[i])
		} else {
			txs[i] = ret.Transaction
		}
	}

	return txs
}

//only return array of avaliable txs
func (ledger *LedgerGlobal) GetTransactionsByID(txIDs []string) []*protos.Transaction {
	ret := ledger.GetTransactionsByIDAligned(txIDs)
	var endpos int
	for _, t := range ret {
		if t != nil {
			ret[endpos] = t
			endpos++
		}
	}

	return ret[:endpos]
}
