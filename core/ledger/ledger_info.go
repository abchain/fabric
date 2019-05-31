package ledger

import (
	"github.com/abchain/fabric/protos"
)

//extended the blockchain info for a ledger

type LedgerInfo struct {
	//base info
	*protos.BlockchainInfo

	//NOTICE following integer fields are all "HEIGHT" of
	//the chain, that is, the final block number PLUS 1
	//the queryable height for main modules,
	//the height of states decide the height
	//in base info
	Avaliable struct {
		Blocks  uint64
		States  uint64
		Indexes uint64
	}

	//the persisted height for main modules,
	//any state above this heigh can be easily
	//rollback to
	//notice some of the fields may higher than
	//current active height, according to the commit
	//strategy it was applied to
	Persisted struct {
		Blocks  uint64
		States  uint64
		Indexes uint64
	}

	States struct {
		//can query state current
		Avaliable bool
		//states's most available hash, if avaliable is false, it shows the
		//targethash it is syncing to, or it has the persisted statehash
		AvaliableHash []byte
	}
}

func (ledger *Ledger) Tag() string {
	t := ledger.blockchain.GetDBTag()
	if t == "" {
		return "default"
	}
	return t
}

func (ledger *Ledger) LedgerStateInfo() (height uint64, hash []byte, avaliable bool) {

	ledger.readCache.RLock()
	defer ledger.readCache.RUnlock()

	height, hash, avaliable = ledger.state.cache.curHeight,
		ledger.state.cache.currentHash, ledger.state.isSyncing()
	return
}

func (ledger *Ledger) GetLedgerInfo() (*LedgerInfo, error) {

	ledger.readCache.RLock()
	defer ledger.readCache.RUnlock()

	baseinfo, err := ledger.blockchain.getBlockchainInfo()
	if err != nil {
		return nil, err
	}

	//consider if block do not include state
	if len(baseinfo.GetCurrentStateHash()) == 0 {
		baseinfo.CurrentStateHash = ledger.state.cache.currentHash
	}

	ret := &LedgerInfo{BlockchainInfo: baseinfo}
	//fill other fields
	if baseinfo.Height > 0 {
		ret.Persisted.Blocks = ledger.blockchain.continuousTo.GetProgress() + 1
		ret.Persisted.Indexes = ledger.index.cache.indexedTo.GetProgress() + 1
	}
	ret.Persisted.States = ledger.state.cache.refHeight

	ret.Avaliable.Blocks = baseinfo.GetHeight()
	ret.Avaliable.States = ledger.state.cache.curHeight
	ret.Avaliable.Indexes = ledger.index.cache.indexedTo.GetTop() + 1

	if b := ledger.state.isSyncing(); b {
		ret.States.Avaliable = false
		ret.States.AvaliableHash = ledger.state.buildingState.statehash
	} else {
		ret.States.Avaliable = true
		ret.States.AvaliableHash = ledger.state.cache.refHash
	}
	return ret, nil

}
