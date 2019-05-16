package protos

import (
	"github.com/golang/protobuf/proto"
)

func (x SyncMsg_Type) OnRecv() string {
	return x.String()
}

func (x SyncMsg_Type) OnSend() string {
	return "SEND_" + x.String()
}

func NewBucketTreeOffset(level, bucketNum uint64) *SyncOffset {
	btOffset := &BucketTreeOffset{Level: level, BucketNum: bucketNum, Delta: 1}

	return &SyncOffset{Data: &SyncOffset_Buckettree{Buckettree: btOffset}}
}

func (m *BucketTreeOffset) Byte() ([]byte, error) {
	return proto.Marshal(m)
}

func UnmarshalBucketTree(b []byte) (*BucketTreeOffset, error) {
	ret := new(BucketTreeOffset)
	return ret, proto.Unmarshal(b, ret)
}

func (m *SyncBlockRange) NextNumOp() func(uint64) uint64 {

	if m.Start > m.End {
		return func(m uint64) uint64 { return m - 1 }
	} else {
		return func(m uint64) uint64 { return m + 1 }
	}
}

func (m *SyncBlockRange) Length() uint64 {
	if m.Start > m.End {
		return m.Start - m.End
	} else {
		return m.End - m.Start
	}
}
