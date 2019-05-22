package ledger

import (
	"github.com/abchain/fabric/protos"
)

//other module can subscribe notify for the updating of the ledger
//currently, only PERSISTED updating will be delivered via subscriptions

//DO NOT call methods of corresponding ledger in callback function
//(if you must do that, use an extra goroiutine), ledger will try to
//ensure the information is complete in notification but subscriber
//should still prepare for the possibility of missing data, including some
//parameters in callback function is nil or some update is not being
//notified

//only the "raw" block (i.e. without transactions) is notified
type LedgerNewBlockNotify func(blkn uint64, blk *protos.Block)
type LedgerNewStateNotify func(statepos uint64, statehash []byte)

func (ledger *Ledger) SubScribeNewBlock(f ...LedgerNewBlockNotify) {
	ledger.readCache.Lock()
	defer ledger.readCache.Unlock()

	ledger.blockchain.updatesubscriptions = append(ledger.blockchain.updatesubscriptions, f...)
}

func (ledger *Ledger) SubScribeNewState(f ...LedgerNewStateNotify) {
	ledger.readCache.Lock()
	defer ledger.readCache.Unlock()

	ledger.state.updatesubscriptions = append(ledger.state.updatesubscriptions, f...)
}
