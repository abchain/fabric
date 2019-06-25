package monologue

import (
	"errors"
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
func BuildConfirm(ccName string, proposalC chan<- framework.ProposalTask,
	deliver framework.ConsensusTxDeliver) func(uint64, []byte) {

	wrapper := DefaultWrapper(ccName, tipMethod)

	return func(n uint64, hash []byte) {
		proposalC <- framework.ProposalTask(func(ctx context.Context, _ framework.LedgerLearnerInfo) {
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

type miner struct {
	metaTagger func(*cspb.PurposeBlock) []byte
	wrapper    func([]byte) (*pb.Transaction, error)
	cancelF    context.CancelFunc
}

func defaultTagger(*cspb.PurposeBlock) []byte {
	return []byte("YAFABRIC MONOLOGUE BUILDER")
}

func NewMiner(wrapper func([]byte) (*pb.Transaction, error)) *miner {

	return &miner{defaultTagger, wrapper, nil}

}

func (p *miner) SetTagger(f func(*cspb.PurposeBlock) []byte) { p.metaTagger = f }

//implement of  framework.ConsensusProposal
func (p *miner) Cancel() {
	if p.cancelF != nil {
		p.cancelF()
		p.cancelF = nil
	}
}

//miner only work when the target ledger is at "top" (i.e.: it has no blocks further than
//current state)
func (p *miner) propose(txes []*pb.TransactionHandlingContext,
	l framework.LedgerLearnerInfo) (*pb.Transaction, error) {

	if p.wrapper == nil {
		panic("No wrapper")
	}

	linfo, err := l.Ledger().GetLedgerInfo()
	if err != nil {
		return nil, err
	}

	if linfo.Avaliable.States < linfo.Avaliable.Blocks {
		return nil, errors.New("State is fallen behind")
	}

	var wctx context.Context
	wctx, p.cancelF = context.WithCancel(context.Background())

	blk := new(cspb.PurposeBlock)
	blk.N = linfo.GetHeight()
	blk.B, err = l.Preview(wctx, blk.N, txes)
	if err != nil {
		return nil, err
	}

	metaData := p.metaTagger(blk)
	//we should also truncate the tx part and non-hash part
	//make it a RAW block
	blk.GetB().ConsensusMetadata = metaData
	blk.GetB().Transactions = nil
	blk.GetB().NonHashData = nil

	payload, err := blk.Bytes()
	if err != nil {
		return nil, err
	}

	return p.wrapper(payload)

}

func (p *miner) Propose(txes cspb.PendingTransactions,
	l framework.LedgerLearnerInfo) <-chan *cspb.ConsensusPurpose {

	outC := make(chan *cspb.ConsensusPurpose, 1)

	tx, err := p.propose(txes.PType(), l)
	if err != nil {
		outC <- &cspb.ConsensusPurpose{
			Out: &cspb.ConsensusPurpose_Error{Error: err.Error()},
		}
	} else {
		outC <- &cspb.ConsensusPurpose{
			Out: &cspb.ConsensusPurpose_Txs{
				Txs: &pb.TransactionBlock{Transactions: []*pb.Transaction{tx}},
			},
		}
	}

	return outC

}
