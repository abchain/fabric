package monologue

import (
	"github.com/abchain/fabric/consensus/framework"
	cspb "github.com/abchain/fabric/consensus/protos"
	"github.com/abchain/fabric/core/chaincode/shim"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
)

type monologueCC struct {
	core            *ConsensusCore
	candidateNotify func(*cspb.PurposeBlock)
}

const tipMethod = "DOTIP"

func NewChaincode(core *ConsensusCore, notify func(*cspb.PurposeBlock)) monologueCC {
	return monologueCC{core, notify}
}

func (cc monologueCC) Init(stub shim.ChaincodeStubInterface, function string, args []string) ([]byte, error) {
	return nil, nil
}

func (cc monologueCC) Invoke(stub shim.ChaincodeStubInterface, function string, args []string) ([]byte, error) {
	panic("Should not called")
}

func (cc monologueCC) Query(stub shim.ChaincodeStubInterface, function string, _ []string) ([]byte, error) {

	pl := cspb.ConsensusPayloads(stub.GetArgs()[1])

	if inputBlock, err := pl.Block(); err != nil {
		return nil, err
	} else {

		if function == tipMethod {
			//see func BuildConfirm
			return cc.core.Tip(inputBlock.GetN()-1,
				inputBlock.GetB().GetPreviousBlockHash()).ToRet()
		} else {

			if cc.candidateNotify != nil {
				cc.candidateNotify(inputBlock)
			}

			return cc.core.ConsensusInput(inputBlock).ToRet()
		}

	}

}

//default wrapper purpose the generated tx is applied on the monologueCC (which should be
//a system cc) directly
func DefaultWrapper(ccname string, method string) func([]byte) (*pb.Transaction, error) {
	return func(payload []byte) (*pb.Transaction, error) {
		return pb.NewChaincodeTransaction(pb.Transaction_CHAINCODE_QUERY,
			&pb.ChaincodeID{Name: ccname}, method, [][]byte{payload})
	}
}

//monologue will "hide" its lastest block but a miner require it to be able to mine
//the next one, the latest block is selected external of our module and here is the
//entry to set the result. A custom mining progress is generated and delivered (so
//core and states can be accessed without racing cases), the latest block can be
//poped up from core and delivered to learner, push the state forward, so miner can
//do mining for the next block. i.e. miner mining on a series of continuous blocks
//should alternately call mining - confirming - mining
//the deliver should direct send tx into local learner instead of the tx network,
//which is provided by framework.baseLearnerImpl
func BuildConfirm(ccName string, purposeC chan<- framework.PurposeTask,
	deliver framework.ConsensusTxDeliver) func(uint64, []byte) {

	wrapper := DefaultWrapper(ccName, tipMethod)

	return func(n uint64, hash []byte) {
		purposeC <- framework.PurposeTask(func(ctx context.Context, _ framework.LedgerLearnerInfo) {
			tipBlock := new(cspb.PurposeBlock)
			tipBlock.N = n + 1
			tipBlock.B = new(pb.Block)
			tipBlock.B.PreviousBlockHash = hash

			payload, err := tipBlock.Bytes()
			if err == nil {
				tx, err := wrapper(payload)
				if err == nil {
					deliver.Send(ctx, []*pb.Transaction{tx})
				}
			}
		})
	}

}

type purposer struct {
	blockWithStateHash bool
	metaTagger         func(*cspb.PurposeBlock) []byte
	wrapper            func([]byte) (*pb.Transaction, error)
}

func defaultTagger(*cspb.PurposeBlock) []byte {
	return []byte("YAFABRIC MONOLOGUE BUILDER")
}

func NewPurposer(withstate bool, wrapper func([]byte) (*pb.Transaction, error)) *purposer {

	return &purposer{withstate, defaultTagger, wrapper}

}

func (p *purposer) SetTagger(f func(*cspb.PurposeBlock) []byte) { p.metaTagger = f }

//implement of  framework.ConsensusPurposer
func (p *purposer) RequireState() bool { return p.blockWithStateHash }
func (p *purposer) Cancel()            {}
func (p *purposer) Purpose(blk *cspb.PurposeBlock) *cspb.ConsensusPurpose {

	if p.wrapper == nil {
		return &cspb.ConsensusPurpose{
			Out: &cspb.ConsensusPurpose_Error{Error: "No wrapper"},
		}
	}

	metaData := p.metaTagger(blk)
	//we should also truncate the tx part and non-hash part
	blk.GetB().ConsensusMetadata = metaData
	blk.GetB().Transactions = nil
	blk.GetB().NonHashData = nil

	payload, err := blk.Bytes()
	if err != nil {
		return &cspb.ConsensusPurpose{
			Out: &cspb.ConsensusPurpose_Error{Error: err.Error()},
		}
	}

	outTx, err := p.wrapper(payload)
	if err != nil {
		return &cspb.ConsensusPurpose{
			Out: &cspb.ConsensusPurpose_Error{Error: err.Error()},
		}
	}

	return &cspb.ConsensusPurpose{
		Out: &cspb.ConsensusPurpose_Txs{
			Txs: &pb.TransactionBlock{Transactions: []*pb.Transaction{outTx}},
		},
	}
}
