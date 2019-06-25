package csprotos

import proto "github.com/golang/protobuf/proto"
import "github.com/golang/protobuf/ptypes/empty"
import pb "github.com/abchain/fabric/protos"

var Nothing = &empty.Empty{}

type PendingTransactions []*pb.TransactionHandlingContext

func (m PendingTransactions) PType() []*pb.TransactionHandlingContext {
	return []*pb.TransactionHandlingContext(m)
}

type ConsensusRet []byte

func (bt ConsensusRet) MinerOut() (*ConsensusPurpose, error) {

	msg := new(ConsensusPurpose)
	if err := proto.Unmarshal(bt, msg); err != nil {
		return nil, err
	}

	return msg, nil
}

func (bt ConsensusRet) Out() (*ConsensusOutput, error) {

	msg := new(ConsensusOutput)
	if err := proto.Unmarshal(bt, msg); err != nil {
		return nil, err
	}

	return msg, nil

}

func (m *ConsensusOutput) ToRet() ([]byte, error) {
	return proto.Marshal(m)
}

func (m *ConsensusPurpose) ToRet() ([]byte, error) {
	return proto.Marshal(m)
}

type ConsensusPayloads []byte

func (bt ConsensusPayloads) Block() (*PurposeBlock, error) {

	msg := new(PurposeBlock)
	if err := proto.Unmarshal(bt, msg); err != nil {
		return nil, err
	}

	return msg, nil

}

func (b *PurposeBlock) Bytes() ([]byte, error) {
	return proto.Marshal(b)
}
