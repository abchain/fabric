package protos

import (
	"github.com/golang/protobuf/proto"
)

func NewBlockOffset(start, end uint64) *SyncOffset {
	offset := &SyncOffset_Block{Block: &BlockOffset{StartNum: start, EndNum: end}}

	return &SyncOffset{Data: offset}
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
