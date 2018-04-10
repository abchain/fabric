package ledger

import (
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/core/ledger/statemgmt/state"

	pb "github.com/abchain/fabric/protos"
)

// BlockChainAccessor interface for retreiving blocks by block number
type BlockChainAccessor interface {
	GetBlockByNumber(blockNumber uint64) (*pb.Block, error)
	GetBlockchainSize() uint64
	GetCurrentStateHash() (stateHash []byte, err error)
}

// BlockChainModifier interface for applying changes to the block chain
type BlockChainModifier interface {
	ApplyStateDelta(id interface{}, delta *statemgmt.StateDelta) error
	RollbackStateDelta(id interface{}) error
	CommitStateDelta(id interface{}) error
	EmptyState() error
	PutBlock(blockNumber uint64, block *pb.Block) error
}

// BlockChainUtil interface for interrogating the block chain
type BlockChainUtil interface {
	HashBlock(block *pb.Block) ([]byte, error)
	VerifyChain(start, finish uint64) (uint64, error)
}

// StateAccessor interface for retreiving blocks by block number
type StateAccessor interface {
	GetStateSnapshot() (*state.StateSnapshot, error)
	GetStateDelta(blockNumber uint64) (*statemgmt.StateDelta, error)
}
