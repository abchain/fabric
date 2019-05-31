package framework_example

import (
	cspb "github.com/abchain/fabric/consensus/protos"
	"github.com/abchain/fabric/core/chaincode/shim"
	pb "github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
)

type SimpleConsensusChaincode struct{}

var DefaultCCName = "basecscc"

func (SimpleConsensusChaincode) Init(stub shim.ChaincodeStubInterface, function string, args []string) ([]byte, error) {
	return nil, nil
}

func (SimpleConsensusChaincode) Invoke(stub shim.ChaincodeStubInterface, function string, args []string) ([]byte, error) {
	panic("Should not called")
	//	return nil, errors.New("Should not called")
}

func (SimpleConsensusChaincode) Query(stub shim.ChaincodeStubInterface, function string, _ []string) ([]byte, error) {

	inputBlock := new(cspb.PurposeBlock)

	if err := proto.Unmarshal(stub.GetArgs()[1], inputBlock); err != nil {
		return nil, err
	}

	return proto.Marshal(&cspb.ConsensusOutput{
		Out:      &cspb.ConsensusOutput_Block{Block: inputBlock.GetB()},
		Position: inputBlock.GetN(),
	})
}

func BuildTransaction(n uint64, b *pb.Block) *pb.Transaction {

	inputBlock := &cspb.PurposeBlock{
		N: n,
		B: b,
	}

	bts, err := proto.Marshal(inputBlock)
	if err != nil {
		panic(err)
	}

	tx, err := pb.NewChaincodeExecute(&pb.ChaincodeInvocationSpec{
		ChaincodeSpec: &pb.ChaincodeSpec{
			Type:        pb.ChaincodeSpec_GOLANG,
			ChaincodeID: &pb.ChaincodeID{Name: DefaultCCName},
			CtorMsg:     &pb.ChaincodeInput{Args: [][]byte{[]byte("call"), bts}},
		},
	}, "", pb.Transaction_CHAINCODE_QUERY) //notice it MUST be query tx

	dig, err := tx.Digest()
	if err != nil {
		panic(err)
	}
	tx.Txid = pb.TxidFromDigest(dig)

	if err != nil {
		panic(err)
	}

	return tx

}
