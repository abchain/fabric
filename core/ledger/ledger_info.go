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
		//states we can query by specified height
		AvaliableTill uint64
		//if not nil, Avaliable is false and this specified
		//the target of state syncing
		Syncing *protos.BlockchainInfo
	}
}

func (ledger *Ledger) GetLedgerInfo() (*LedgerInfo, error) {
	return nil, ErrResourceNotFound

}
