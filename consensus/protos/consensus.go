package csprotos

import proto "github.com/golang/protobuf/proto"

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
