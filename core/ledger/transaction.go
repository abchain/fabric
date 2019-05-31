package ledger

import (
	"errors"
	"github.com/abchain/fabric/core/db"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"sync"
)

// Transaction work for get/put transaction and holds "unconfirmed" transaction
// (with the handling context)
type transactionPool struct {
	sync.RWMutex
	txPool map[string]*pb.TransactionHandlingContext
	//use for temporary reading in a long-journey
	txPoolSnapshot map[string]*pb.TransactionHandlingContext

	commitHooks []func([]string, uint64)

	//TODO: should we add cache for reading transaction (especially for which
	//is commited recently)
}

func newTxPool() (*transactionPool, error) {

	txpool := &transactionPool{}

	txpool.txPool = make(map[string]*pb.TransactionHandlingContext)
	return txpool, nil
}

//deprecated: this entry is only for test purposed
func (tp *transactionPool) poolTransaction(preh pb.TxPreHandler, txs []*pb.Transaction) {

	tp.Lock()
	defer tp.Unlock()

	for _, tx := range txs {
		te, err := preh.Handle(pb.NewTransactionHandlingContext(tx))
		if err != nil {
			ledgerLogger.Debug("create txe fail:", err)
			continue
		}
		tp.txPool[tx.GetTxid()] = te
		ledgerLogger.Debugf("pool tx [%s:%p]", tx.GetTxid(), tx)
	}
	ledgerLogger.Debugf("currently %d txs is pooled", len(tp.txPool)+len(tp.txPoolSnapshot))
}

func (tp *transactionPool) poolTxe(txe *pb.TransactionHandlingContext) {

	tp.Lock()
	defer tp.Unlock()

	tp.txPool[txe.GetTxid()] = txe
	ledgerLogger.Debugf("pool tx [%s], currently %d txs is pooled", txe.GetTxid(), len(tp.txPool)+len(tp.txPoolSnapshot))
}

func removeOneTx(pool map[string]*pb.TransactionHandlingContext, id string) {
	delete(pool, id)
}

func (tp *transactionPool) cleanTransaction(txs []*pb.Transaction) {
	tp.Lock()
	defer tp.Unlock()
	if tp.txPoolSnapshot == nil {
		for _, tx := range txs {
			removeOneTx(tp.txPool, tx.GetTxid())
		}
		ledgerLogger.Debugf("%d txs has been pruned from pool later, currently %d txs left", len(txs), len(tp.txPool)+len(tp.txPoolSnapshot))
	} else {
		for _, tx := range txs {
			tp.txPool[tx.GetTxid()] = nil
		}
		ledgerLogger.Debugf("%d txs has will be pruned from pool later, currently %d txs left", len(txs), len(tp.txPool)+len(tp.txPoolSnapshot))
	}

}

func (tp *transactionPool) commitTransaction(txids []string, blockNum uint64) error {

	//notice: all the txids, not filter after txpool, is passed to commitHook
	for _, hf := range tp.commitHooks {
		hf(txids, blockNum)
	}

	pendingTxs := make([]*pb.Transaction, 0, len(txids))

	tp.Lock()
	if tp.txPoolSnapshot == nil {
		for _, id := range txids {
			if tx, ok := tp.txPool[id]; ok {
				pendingTxs = append(pendingTxs, tx.Transaction)
				removeOneTx(tp.txPool, id)
			}
			ledgerLogger.Debugf("%d txs will be commited, currently %d txs left", len(pendingTxs), len(tp.txPool)+len(tp.txPoolSnapshot))
		}
	} else {
		for _, id := range txids {
			tx, ok := tp.txPool[id]
			if !ok {
				tx, ok = tp.txPoolSnapshot[id]
				if ok {
					tp.txPool[id] = nil
				} else {
					continue
				}
			} else {
				delete(tp.txPool, id)
			}
			pendingTxs = append(pendingTxs, tx.Transaction)
			ledgerLogger.Debugf("%d txs will be commited, currently %d(+%d snapshotted) txs left ", len(pendingTxs), len(tp.txPool), len(tp.txPoolSnapshot))
		}
	}

	tp.Unlock()
	return db.GetGlobalDBHandle().PutTransactions(pendingTxs)
}

func (tp *transactionPool) getConfirmedTransaction(txID string) (*pb.Transaction, error) {
	return fetchTxFromDB(txID)
}

func (tp *transactionPool) finishIteration(out chan *pb.TransactionHandlingContext) {
	defer close(out)
	tp.Lock()
	defer tp.Unlock()

	//we suppose the snapshot is larger than the (temporary generated) txPool
	for id, txe := range tp.txPool {
		if txe != nil {
			tp.txPoolSnapshot[id] = txe
		} else {
			removeOneTx(tp.txPoolSnapshot, id)
		}
	}

	//switch
	tp.txPool = tp.txPoolSnapshot
	tp.txPoolSnapshot = nil
}

func (tp *transactionPool) getPooledTxCount() int {
	tp.RLock()
	defer tp.RUnlock()

	ledgerLogger.Debugf("%v, %v", tp.txPool, tp.txPoolSnapshot)

	return len(tp.txPool) + len(tp.txPoolSnapshot)
}

func (tp *transactionPool) getPooledTx(txid string) *pb.TransactionHandlingContext {
	return tp.getPooledTxs([]string{txid})[0]
}

//take pooled txs, ensure each request has corresponding entry (maybe nil)
//in the response array
func (tp *transactionPool) getPooledTxs(txids []string) (ret []*pb.TransactionHandlingContext) {
	tp.RLock()
	defer tp.RUnlock()

	for _, txid := range txids {
		txe, ok := tp.txPool[txid]
		if !ok && tp.txPoolSnapshot != nil {
			txe = tp.txPoolSnapshot[txid]
		}

		ret = append(ret, txe)
	}

	return
}

//only one long-journey read is allowed once
func (tp *transactionPool) iteratePooledTx(ctx context.Context) (chan *pb.TransactionHandlingContext, error) {

	tp.Lock()
	defer tp.Unlock()

	if tp.txPoolSnapshot != nil {
		return nil, errors.New("Iterate-read request duplicated")
	}

	tp.txPoolSnapshot = tp.txPool
	tp.txPool = make(map[string]*pb.TransactionHandlingContext)

	out := make(chan *pb.TransactionHandlingContext)
	go func(pool map[string]*pb.TransactionHandlingContext) {
		defer tp.finishIteration(out)
		for _, tx := range pool {
			select {
			case out <- tx:
			case <-ctx.Done():
				return
			}
		}
	}(tp.txPoolSnapshot)

	return out, nil
}

//shared by block struct's access
func fetchTxFromDB(txID string) (*pb.Transaction, error) {
	return db.GetGlobalDBHandle().GetTransaction(txID)
}

func fetchTxsFromDB(txIDs []string) []*pb.Transaction {
	return db.GetGlobalDBHandle().GetTransactions(txIDs)
}

func putTxsToDB(txs []*pb.Transaction) error {
	return db.GetGlobalDBHandle().PutTransactions(txs)
}
