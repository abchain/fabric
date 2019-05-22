package ledger

import (
	"github.com/abchain/fabric/protos"
)

//extended the blockchain info for a ledger

type LedgerInfo struct {
	//base info
	*protos.BlockchainInfo

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

func (ledger *Ledger) GetLedgerInfo() (*LedgerInfo, error) {

	ledger.readCache.RLock()
	defer ledger.readCache.RUnlock()

	baseinfo, err := ledger.blockchain.getBlockchainInfo()
	if err != nil {
		return nil, err
	}

	ret := &LedgerInfo{BlockchainInfo: baseinfo}
	//fill other fields
	if baseinfo.Height > 0 {
		ret.Persisted.Blocks = ledger.blockchain.continuousTo.GetProgress() + 1
		ret.Persisted.Indexes = ledger.index.cache.indexedTo.GetProgress() + 1
	}
	ret.Persisted.States = ledger.state.cache.refHeight

	if h := baseinfo.GetHeight(); h > 0 {
		ret.Avaliable.Blocks = h - 1
	}
	ret.Avaliable.States = ledger.state.cache.curHeight
	ret.Avaliable.Indexes = ledger.index.cache.indexedTo.GetTop()

	if b := ledger.state.isSyncing(); b {
		ret.States.Avaliable = b
		ret.States.AvaliableHash = ledger.state.buildingState.statehash
	} else {
		ret.States.Avaliable = b
		ret.States.AvaliableHash = ledger.state.cache.refHash
	}
	return ret, nil

}
