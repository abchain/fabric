// Code generated by protoc-gen-go. DO NOT EDIT.
// source: sync.proto

package protos

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"

import (
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

type SyncType int32

const (
	SyncType_UNDEFINED      SyncType = 0
	SyncType_SYNC_BLOCK     SyncType = 1
	SyncType_SYNC_BLOCK_ACK SyncType = 2
	SyncType_SYNC_STATE     SyncType = 3
	SyncType_SYNC_STATE_ACK SyncType = 4
)

var SyncType_name = map[int32]string{
	0: "UNDEFINED",
	1: "SYNC_BLOCK",
	2: "SYNC_BLOCK_ACK",
	3: "SYNC_STATE",
	4: "SYNC_STATE_ACK",
}
var SyncType_value = map[string]int32{
	"UNDEFINED":      0,
	"SYNC_BLOCK":     1,
	"SYNC_BLOCK_ACK": 2,
	"SYNC_STATE":     3,
	"SYNC_STATE_ACK": 4,
}

func (x SyncType) String() string {
	return proto.EnumName(SyncType_name, int32(x))
}
func (SyncType) EnumDescriptor() ([]byte, []int) { return fileDescriptor8, []int{0} }

type SyncMsg_Type int32

const (
	SyncMsg_UNDEFINED                     SyncMsg_Type = 0
	SyncMsg_SYNC_STATE_NOTIFY             SyncMsg_Type = 1
	SyncMsg_SYNC_STATE_OPT                SyncMsg_Type = 2
	SyncMsg_SYNC_SESSION_START            SyncMsg_Type = 3
	SyncMsg_SYNC_SESSION_START_ACK        SyncMsg_Type = 4
	SyncMsg_SYNC_SESSION_QUERY            SyncMsg_Type = 5
	SyncMsg_SYNC_SESSION_QUERY_ACK        SyncMsg_Type = 6
	SyncMsg_SYNC_SESSION_END              SyncMsg_Type = 8
	SyncMsg_SYNC_SESSION_DELTAS           SyncMsg_Type = 16
	SyncMsg_SYNC_SESSION_DELTAS_ACK       SyncMsg_Type = 17
	SyncMsg_SYNC_SESSION_SYNC_MESSAGE     SyncMsg_Type = 18
	SyncMsg_SYNC_SESSION_SYNC_MESSAGE_ACK SyncMsg_Type = 19
)

var SyncMsg_Type_name = map[int32]string{
	0:  "UNDEFINED",
	1:  "SYNC_STATE_NOTIFY",
	2:  "SYNC_STATE_OPT",
	3:  "SYNC_SESSION_START",
	4:  "SYNC_SESSION_START_ACK",
	5:  "SYNC_SESSION_QUERY",
	6:  "SYNC_SESSION_QUERY_ACK",
	8:  "SYNC_SESSION_END",
	16: "SYNC_SESSION_DELTAS",
	17: "SYNC_SESSION_DELTAS_ACK",
	18: "SYNC_SESSION_SYNC_MESSAGE",
	19: "SYNC_SESSION_SYNC_MESSAGE_ACK",
}
var SyncMsg_Type_value = map[string]int32{
	"UNDEFINED":                     0,
	"SYNC_STATE_NOTIFY":             1,
	"SYNC_STATE_OPT":                2,
	"SYNC_SESSION_START":            3,
	"SYNC_SESSION_START_ACK":        4,
	"SYNC_SESSION_QUERY":            5,
	"SYNC_SESSION_QUERY_ACK":        6,
	"SYNC_SESSION_END":              8,
	"SYNC_SESSION_DELTAS":           16,
	"SYNC_SESSION_DELTAS_ACK":       17,
	"SYNC_SESSION_SYNC_MESSAGE":     18,
	"SYNC_SESSION_SYNC_MESSAGE_ACK": 19,
}

func (x SyncMsg_Type) String() string {
	return proto.EnumName(SyncMsg_Type_name, int32(x))
}
func (SyncMsg_Type) EnumDescriptor() ([]byte, []int) { return fileDescriptor8, []int{9, 0} }

type GlobalState struct {
	Count                   uint64   `protobuf:"varint,1,opt,name=count" json:"count,omitempty"`
	NextNodeStateHash       [][]byte `protobuf:"bytes,3,rep,name=nextNodeStateHash,proto3" json:"nextNodeStateHash,omitempty"`
	ParentNodeStateHash     [][]byte `protobuf:"bytes,4,rep,name=parentNodeStateHash,proto3" json:"parentNodeStateHash,omitempty"`
	LastBranchNodeStateHash []byte   `protobuf:"bytes,5,opt,name=lastBranchNodeStateHash,proto3" json:"lastBranchNodeStateHash,omitempty"`
	NextBranchNodeStateHash []byte   `protobuf:"bytes,7,opt,name=nextBranchNodeStateHash,proto3" json:"nextBranchNodeStateHash,omitempty"`
}

func (m *GlobalState) Reset()                    { *m = GlobalState{} }
func (m *GlobalState) String() string            { return proto.CompactTextString(m) }
func (*GlobalState) ProtoMessage()               {}
func (*GlobalState) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{0} }

func (m *GlobalState) GetCount() uint64 {
	if m != nil {
		return m.Count
	}
	return 0
}

func (m *GlobalState) GetNextNodeStateHash() [][]byte {
	if m != nil {
		return m.NextNodeStateHash
	}
	return nil
}

func (m *GlobalState) GetParentNodeStateHash() [][]byte {
	if m != nil {
		return m.ParentNodeStateHash
	}
	return nil
}

func (m *GlobalState) GetLastBranchNodeStateHash() []byte {
	if m != nil {
		return m.LastBranchNodeStateHash
	}
	return nil
}

func (m *GlobalState) GetNextBranchNodeStateHash() []byte {
	if m != nil {
		return m.NextBranchNodeStateHash
	}
	return nil
}

// BlockState is the payload of Message.SYNC_BLOCK_ADDED. When a VP
// commits a new block to the ledger, it will notify its connected NVPs of the
// block and the delta state. The NVP may call the ledger APIs to apply the
// block and the delta state to its ledger if the block's previousBlockHash
// equals to the NVP's current block hash
type BlockState struct {
	Block      *Block `protobuf:"bytes,1,opt,name=block" json:"block,omitempty"`
	StateDelta []byte `protobuf:"bytes,2,opt,name=stateDelta,proto3" json:"stateDelta,omitempty"`
	Height     uint64 `protobuf:"varint,3,opt,name=Height" json:"Height,omitempty"`
}

func (m *BlockState) Reset()                    { *m = BlockState{} }
func (m *BlockState) String() string            { return proto.CompactTextString(m) }
func (*BlockState) ProtoMessage()               {}
func (*BlockState) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{1} }

func (m *BlockState) GetBlock() *Block {
	if m != nil {
		return m.Block
	}
	return nil
}

func (m *BlockState) GetStateDelta() []byte {
	if m != nil {
		return m.StateDelta
	}
	return nil
}

func (m *BlockState) GetHeight() uint64 {
	if m != nil {
		return m.Height
	}
	return 0
}

// SyncBlockRange is the payload of Message.SYNC_GET_BLOCKS, where
// start and end indicate the starting and ending blocks inclusively. The order
// in which blocks are returned is defined by the start and end values. For
// example, if start=3 and end=5, the order of blocks will be 3, 4, 5.
// If start=5 and end=3, the order will be 5, 4, 3.
type SyncBlockRange struct {
	CorrelationId uint64 `protobuf:"varint,1,opt,name=correlationId" json:"correlationId,omitempty"`
	Start         uint64 `protobuf:"varint,2,opt,name=start" json:"start,omitempty"`
	End           uint64 `protobuf:"varint,3,opt,name=end" json:"end,omitempty"`
}

func (m *SyncBlockRange) Reset()                    { *m = SyncBlockRange{} }
func (m *SyncBlockRange) String() string            { return proto.CompactTextString(m) }
func (*SyncBlockRange) ProtoMessage()               {}
func (*SyncBlockRange) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{2} }

func (m *SyncBlockRange) GetCorrelationId() uint64 {
	if m != nil {
		return m.CorrelationId
	}
	return 0
}

func (m *SyncBlockRange) GetStart() uint64 {
	if m != nil {
		return m.Start
	}
	return 0
}

func (m *SyncBlockRange) GetEnd() uint64 {
	if m != nil {
		return m.End
	}
	return 0
}

// SyncBlocks is the payload of Message.SYNC_BLOCKS, where the range
// indicates the blocks responded to the request SYNC_GET_BLOCKS
type SyncBlocks struct {
	Range  *SyncBlockRange `protobuf:"bytes,1,opt,name=range" json:"range,omitempty"`
	Blocks []*Block        `protobuf:"bytes,2,rep,name=blocks" json:"blocks,omitempty"`
}

func (m *SyncBlocks) Reset()                    { *m = SyncBlocks{} }
func (m *SyncBlocks) String() string            { return proto.CompactTextString(m) }
func (*SyncBlocks) ProtoMessage()               {}
func (*SyncBlocks) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{3} }

func (m *SyncBlocks) GetRange() *SyncBlockRange {
	if m != nil {
		return m.Range
	}
	return nil
}

func (m *SyncBlocks) GetBlocks() []*Block {
	if m != nil {
		return m.Blocks
	}
	return nil
}

// SyncSnapshotRequest Payload for the penchainMessage.SYNC_GET_SNAPSHOT message.
type SyncStateSnapshotRequest struct {
	CorrelationId uint64 `protobuf:"varint,1,opt,name=correlationId" json:"correlationId,omitempty"`
}

func (m *SyncStateSnapshotRequest) Reset()                    { *m = SyncStateSnapshotRequest{} }
func (m *SyncStateSnapshotRequest) String() string            { return proto.CompactTextString(m) }
func (*SyncStateSnapshotRequest) ProtoMessage()               {}
func (*SyncStateSnapshotRequest) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{4} }

func (m *SyncStateSnapshotRequest) GetCorrelationId() uint64 {
	if m != nil {
		return m.CorrelationId
	}
	return 0
}

// SyncStateSnapshot is the payload of Message.SYNC_SNAPSHOT, which is a response
// to penchainMessage.SYNC_GET_SNAPSHOT. It contains the snapshot or a chunk of the
// snapshot on stream, and in which case, the sequence indicate the order
// starting at 0.  The terminating message will have len(delta) == 0.
type SyncStateSnapshot struct {
	Delta       []byte                    `protobuf:"bytes,1,opt,name=delta,proto3" json:"delta,omitempty"`
	Sequence    uint64                    `protobuf:"varint,2,opt,name=sequence" json:"sequence,omitempty"`
	BlockNumber uint64                    `protobuf:"varint,3,opt,name=blockNumber" json:"blockNumber,omitempty"`
	Request     *SyncStateSnapshotRequest `protobuf:"bytes,4,opt,name=request" json:"request,omitempty"`
}

func (m *SyncStateSnapshot) Reset()                    { *m = SyncStateSnapshot{} }
func (m *SyncStateSnapshot) String() string            { return proto.CompactTextString(m) }
func (*SyncStateSnapshot) ProtoMessage()               {}
func (*SyncStateSnapshot) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{5} }

func (m *SyncStateSnapshot) GetDelta() []byte {
	if m != nil {
		return m.Delta
	}
	return nil
}

func (m *SyncStateSnapshot) GetSequence() uint64 {
	if m != nil {
		return m.Sequence
	}
	return 0
}

func (m *SyncStateSnapshot) GetBlockNumber() uint64 {
	if m != nil {
		return m.BlockNumber
	}
	return 0
}

func (m *SyncStateSnapshot) GetRequest() *SyncStateSnapshotRequest {
	if m != nil {
		return m.Request
	}
	return nil
}

// SyncStateDeltasRequest is the payload of Message.SYNC_GET_STATE.
// blockNumber indicates the block number for the delta which is being
// requested. If no payload is included with SYNC_GET_STATE, it represents
// a request for a snapshot of the current state.
type SyncStateDeltasRequest struct {
	Range *SyncBlockRange `protobuf:"bytes,1,opt,name=range" json:"range,omitempty"`
}

func (m *SyncStateDeltasRequest) Reset()                    { *m = SyncStateDeltasRequest{} }
func (m *SyncStateDeltasRequest) String() string            { return proto.CompactTextString(m) }
func (*SyncStateDeltasRequest) ProtoMessage()               {}
func (*SyncStateDeltasRequest) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{6} }

func (m *SyncStateDeltasRequest) GetRange() *SyncBlockRange {
	if m != nil {
		return m.Range
	}
	return nil
}

// SyncStateDeltas is the payload of the Message.SYNC_STATE in response to
// the Message.SYNC_GET_STATE message.
type SyncStateDeltas struct {
	Range  *SyncBlockRange `protobuf:"bytes,1,opt,name=range" json:"range,omitempty"`
	Deltas [][]byte        `protobuf:"bytes,2,rep,name=deltas,proto3" json:"deltas,omitempty"`
}

func (m *SyncStateDeltas) Reset()                    { *m = SyncStateDeltas{} }
func (m *SyncStateDeltas) String() string            { return proto.CompactTextString(m) }
func (*SyncStateDeltas) ProtoMessage()               {}
func (*SyncStateDeltas) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{7} }

func (m *SyncStateDeltas) GetRange() *SyncBlockRange {
	if m != nil {
		return m.Range
	}
	return nil
}

func (m *SyncStateDeltas) GetDeltas() [][]byte {
	if m != nil {
		return m.Deltas
	}
	return nil
}

type SyncBlockState struct {
	Range    *SyncBlockRange `protobuf:"bytes,1,opt,name=range" json:"range,omitempty"`
	Syncdata []*BlockState   `protobuf:"bytes,2,rep,name=syncdata" json:"syncdata,omitempty"`
}

func (m *SyncBlockState) Reset()                    { *m = SyncBlockState{} }
func (m *SyncBlockState) String() string            { return proto.CompactTextString(m) }
func (*SyncBlockState) ProtoMessage()               {}
func (*SyncBlockState) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{8} }

func (m *SyncBlockState) GetRange() *SyncBlockRange {
	if m != nil {
		return m.Range
	}
	return nil
}

func (m *SyncBlockState) GetSyncdata() []*BlockState {
	if m != nil {
		return m.Syncdata
	}
	return nil
}

// Like chat, stateSync wrap messages used in a syncing session
type SyncMsg struct {
	Type          SyncMsg_Type `protobuf:"varint,1,opt,name=type,enum=protos.SyncMsg_Type" json:"type,omitempty"`
	CorrelationId uint64       `protobuf:"varint,2,opt,name=correlationId" json:"correlationId,omitempty"`
	Payload       []byte       `protobuf:"bytes,3,opt,name=payload,proto3" json:"payload,omitempty"`
}

func (m *SyncMsg) Reset()                    { *m = SyncMsg{} }
func (m *SyncMsg) String() string            { return proto.CompactTextString(m) }
func (*SyncMsg) ProtoMessage()               {}
func (*SyncMsg) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{9} }

func (m *SyncMsg) GetType() SyncMsg_Type {
	if m != nil {
		return m.Type
	}
	return SyncMsg_UNDEFINED
}

func (m *SyncMsg) GetCorrelationId() uint64 {
	if m != nil {
		return m.CorrelationId
	}
	return 0
}

func (m *SyncMsg) GetPayload() []byte {
	if m != nil {
		return m.Payload
	}
	return nil
}

// //////////////////////////////
type SyncStateQuery struct {
	Id          uint32 `protobuf:"varint,1,opt,name=id" json:"id,omitempty"`
	BlockHeight uint64 `protobuf:"varint,2,opt,name=blockHeight" json:"blockHeight,omitempty"`
}

func (m *SyncStateQuery) Reset()                    { *m = SyncStateQuery{} }
func (m *SyncStateQuery) String() string            { return proto.CompactTextString(m) }
func (*SyncStateQuery) ProtoMessage()               {}
func (*SyncStateQuery) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{10} }

func (m *SyncStateQuery) GetId() uint32 {
	if m != nil {
		return m.Id
	}
	return 0
}

func (m *SyncStateQuery) GetBlockHeight() uint64 {
	if m != nil {
		return m.BlockHeight
	}
	return 0
}

type SyncStateResp struct {
	Id          uint32 `protobuf:"varint,1,opt,name=id" json:"id,omitempty"`
	Postive     bool   `protobuf:"varint,3,opt,name=postive" json:"postive,omitempty"`
	Statehash   []byte `protobuf:"bytes,4,opt,name=statehash,proto3" json:"statehash,omitempty"`
	BlockHeight uint64 `protobuf:"varint,5,opt,name=blockHeight" json:"blockHeight,omitempty"`
}

func (m *SyncStateResp) Reset()                    { *m = SyncStateResp{} }
func (m *SyncStateResp) String() string            { return proto.CompactTextString(m) }
func (*SyncStateResp) ProtoMessage()               {}
func (*SyncStateResp) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{11} }

func (m *SyncStateResp) GetId() uint32 {
	if m != nil {
		return m.Id
	}
	return 0
}

func (m *SyncStateResp) GetPostive() bool {
	if m != nil {
		return m.Postive
	}
	return false
}

func (m *SyncStateResp) GetStatehash() []byte {
	if m != nil {
		return m.Statehash
	}
	return nil
}

func (m *SyncStateResp) GetBlockHeight() uint64 {
	if m != nil {
		return m.BlockHeight
	}
	return 0
}

type SyncStartRequest struct {
	Id          uint32   `protobuf:"varint,1,opt,name=id" json:"id,omitempty"`
	PayloadType SyncType `protobuf:"varint,2,opt,name=payloadType,enum=protos.SyncType" json:"payloadType,omitempty"`
	Payload     []byte   `protobuf:"bytes,3,opt,name=payload,proto3" json:"payload,omitempty"`
}

func (m *SyncStartRequest) Reset()                    { *m = SyncStartRequest{} }
func (m *SyncStartRequest) String() string            { return proto.CompactTextString(m) }
func (*SyncStartRequest) ProtoMessage()               {}
func (*SyncStartRequest) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{12} }

func (m *SyncStartRequest) GetId() uint32 {
	if m != nil {
		return m.Id
	}
	return 0
}

func (m *SyncStartRequest) GetPayloadType() SyncType {
	if m != nil {
		return m.PayloadType
	}
	return SyncType_UNDEFINED
}

func (m *SyncStartRequest) GetPayload() []byte {
	if m != nil {
		return m.Payload
	}
	return nil
}

type SyncStartResponse struct {
	Id             uint32 `protobuf:"varint,1,opt,name=id" json:"id,omitempty"`
	RejectedReason string `protobuf:"bytes,2,opt,name=rejectedReason" json:"rejectedReason,omitempty"`
	Statehash      []byte `protobuf:"bytes,4,opt,name=statehash,proto3" json:"statehash,omitempty"`
	BlockHeight    uint64 `protobuf:"varint,5,opt,name=blockHeight" json:"blockHeight,omitempty"`
}

func (m *SyncStartResponse) Reset()                    { *m = SyncStartResponse{} }
func (m *SyncStartResponse) String() string            { return proto.CompactTextString(m) }
func (*SyncStartResponse) ProtoMessage()               {}
func (*SyncStartResponse) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{13} }

func (m *SyncStartResponse) GetId() uint32 {
	if m != nil {
		return m.Id
	}
	return 0
}

func (m *SyncStartResponse) GetRejectedReason() string {
	if m != nil {
		return m.RejectedReason
	}
	return ""
}

func (m *SyncStartResponse) GetStatehash() []byte {
	if m != nil {
		return m.Statehash
	}
	return nil
}

func (m *SyncStartResponse) GetBlockHeight() uint64 {
	if m != nil {
		return m.BlockHeight
	}
	return 0
}

type UpdatedValue struct {
	Value         []byte `protobuf:"bytes,1,opt,name=Value,proto3" json:"Value,omitempty"`
	PreviousValue []byte `protobuf:"bytes,2,opt,name=PreviousValue,proto3" json:"PreviousValue,omitempty"`
}

func (m *UpdatedValue) Reset()                    { *m = UpdatedValue{} }
func (m *UpdatedValue) String() string            { return proto.CompactTextString(m) }
func (*UpdatedValue) ProtoMessage()               {}
func (*UpdatedValue) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{14} }

func (m *UpdatedValue) GetValue() []byte {
	if m != nil {
		return m.Value
	}
	return nil
}

func (m *UpdatedValue) GetPreviousValue() []byte {
	if m != nil {
		return m.PreviousValue
	}
	return nil
}

type ChaincodeStateDelta struct {
	UpdatedKVs map[string]*UpdatedValue `protobuf:"bytes,1,rep,name=UpdatedKVs" json:"UpdatedKVs,omitempty" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"bytes,2,opt,name=value"`
}

func (m *ChaincodeStateDelta) Reset()                    { *m = ChaincodeStateDelta{} }
func (m *ChaincodeStateDelta) String() string            { return proto.CompactTextString(m) }
func (*ChaincodeStateDelta) ProtoMessage()               {}
func (*ChaincodeStateDelta) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{15} }

func (m *ChaincodeStateDelta) GetUpdatedKVs() map[string]*UpdatedValue {
	if m != nil {
		return m.UpdatedKVs
	}
	return nil
}

type SyncStateChunk struct {
	Offset               *SyncOffset                     `protobuf:"bytes,1,opt,name=offset" json:"offset,omitempty"`
	Roothash             []byte                          `protobuf:"bytes,2,opt,name=roothash,proto3" json:"roothash,omitempty"`
	ChaincodeStateDeltas map[string]*ChaincodeStateDelta `protobuf:"bytes,3,rep,name=ChaincodeStateDeltas" json:"ChaincodeStateDeltas,omitempty" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"bytes,2,opt,name=value"`
}

func (m *SyncStateChunk) Reset()                    { *m = SyncStateChunk{} }
func (m *SyncStateChunk) String() string            { return proto.CompactTextString(m) }
func (*SyncStateChunk) ProtoMessage()               {}
func (*SyncStateChunk) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{16} }

func (m *SyncStateChunk) GetOffset() *SyncOffset {
	if m != nil {
		return m.Offset
	}
	return nil
}

func (m *SyncStateChunk) GetRoothash() []byte {
	if m != nil {
		return m.Roothash
	}
	return nil
}

func (m *SyncStateChunk) GetChaincodeStateDeltas() map[string]*ChaincodeStateDelta {
	if m != nil {
		return m.ChaincodeStateDeltas
	}
	return nil
}

type SyncMessage struct {
	PayloadType   SyncType    `protobuf:"varint,1,opt,name=payloadType,enum=protos.SyncType" json:"payloadType,omitempty"`
	Payload       []byte      `protobuf:"bytes,2,opt,name=payload,proto3" json:"payload,omitempty"`
	Offset        *SyncOffset `protobuf:"bytes,3,opt,name=offset" json:"offset,omitempty"`
	CorrelationId uint64      `protobuf:"varint,4,opt,name=correlationId" json:"correlationId,omitempty"`
	FailedReason  string      `protobuf:"bytes,5,opt,name=failedReason" json:"failedReason,omitempty"`
}

func (m *SyncMessage) Reset()                    { *m = SyncMessage{} }
func (m *SyncMessage) String() string            { return proto.CompactTextString(m) }
func (*SyncMessage) ProtoMessage()               {}
func (*SyncMessage) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{17} }

func (m *SyncMessage) GetPayloadType() SyncType {
	if m != nil {
		return m.PayloadType
	}
	return SyncType_UNDEFINED
}

func (m *SyncMessage) GetPayload() []byte {
	if m != nil {
		return m.Payload
	}
	return nil
}

func (m *SyncMessage) GetOffset() *SyncOffset {
	if m != nil {
		return m.Offset
	}
	return nil
}

func (m *SyncMessage) GetCorrelationId() uint64 {
	if m != nil {
		return m.CorrelationId
	}
	return 0
}

func (m *SyncMessage) GetFailedReason() string {
	if m != nil {
		return m.FailedReason
	}
	return ""
}

type SyncState struct {
	Statehash []byte      `protobuf:"bytes,1,opt,name=statehash,proto3" json:"statehash,omitempty"`
	Offset    *SyncOffset `protobuf:"bytes,2,opt,name=offset" json:"offset,omitempty"`
}

func (m *SyncState) Reset()                    { *m = SyncState{} }
func (m *SyncState) String() string            { return proto.CompactTextString(m) }
func (*SyncState) ProtoMessage()               {}
func (*SyncState) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{18} }

func (m *SyncState) GetStatehash() []byte {
	if m != nil {
		return m.Statehash
	}
	return nil
}

func (m *SyncState) GetOffset() *SyncOffset {
	if m != nil {
		return m.Offset
	}
	return nil
}

type SyncOffset struct {
	Data []byte `protobuf:"bytes,1,opt,name=data,proto3" json:"data,omitempty"`
}

func (m *SyncOffset) Reset()                    { *m = SyncOffset{} }
func (m *SyncOffset) String() string            { return proto.CompactTextString(m) }
func (*SyncOffset) ProtoMessage()               {}
func (*SyncOffset) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{19} }

func (m *SyncOffset) GetData() []byte {
	if m != nil {
		return m.Data
	}
	return nil
}

type BucketTreeOffset struct {
	Level     uint64 `protobuf:"varint,1,opt,name=level" json:"level,omitempty"`
	BucketNum uint64 `protobuf:"varint,2,opt,name=bucketNum" json:"bucketNum,omitempty"`
	Delta     uint64 `protobuf:"varint,3,opt,name=delta" json:"delta,omitempty"`
}

func (m *BucketTreeOffset) Reset()                    { *m = BucketTreeOffset{} }
func (m *BucketTreeOffset) String() string            { return proto.CompactTextString(m) }
func (*BucketTreeOffset) ProtoMessage()               {}
func (*BucketTreeOffset) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{20} }

func (m *BucketTreeOffset) GetLevel() uint64 {
	if m != nil {
		return m.Level
	}
	return 0
}

func (m *BucketTreeOffset) GetBucketNum() uint64 {
	if m != nil {
		return m.BucketNum
	}
	return 0
}

func (m *BucketTreeOffset) GetDelta() uint64 {
	if m != nil {
		return m.Delta
	}
	return 0
}

type BlockOffset struct {
	StartNum uint64 `protobuf:"varint,1,opt,name=startNum" json:"startNum,omitempty"`
	EndNum   uint64 `protobuf:"varint,2,opt,name=endNum" json:"endNum,omitempty"`
}

func (m *BlockOffset) Reset()                    { *m = BlockOffset{} }
func (m *BlockOffset) String() string            { return proto.CompactTextString(m) }
func (*BlockOffset) ProtoMessage()               {}
func (*BlockOffset) Descriptor() ([]byte, []int) { return fileDescriptor8, []int{21} }

func (m *BlockOffset) GetStartNum() uint64 {
	if m != nil {
		return m.StartNum
	}
	return 0
}

func (m *BlockOffset) GetEndNum() uint64 {
	if m != nil {
		return m.EndNum
	}
	return 0
}

func init() {
	proto.RegisterType((*GlobalState)(nil), "protos.GlobalState")
	proto.RegisterType((*BlockState)(nil), "protos.BlockState")
	proto.RegisterType((*SyncBlockRange)(nil), "protos.SyncBlockRange")
	proto.RegisterType((*SyncBlocks)(nil), "protos.SyncBlocks")
	proto.RegisterType((*SyncStateSnapshotRequest)(nil), "protos.SyncStateSnapshotRequest")
	proto.RegisterType((*SyncStateSnapshot)(nil), "protos.SyncStateSnapshot")
	proto.RegisterType((*SyncStateDeltasRequest)(nil), "protos.SyncStateDeltasRequest")
	proto.RegisterType((*SyncStateDeltas)(nil), "protos.SyncStateDeltas")
	proto.RegisterType((*SyncBlockState)(nil), "protos.SyncBlockState")
	proto.RegisterType((*SyncMsg)(nil), "protos.SyncMsg")
	proto.RegisterType((*SyncStateQuery)(nil), "protos.SyncStateQuery")
	proto.RegisterType((*SyncStateResp)(nil), "protos.SyncStateResp")
	proto.RegisterType((*SyncStartRequest)(nil), "protos.SyncStartRequest")
	proto.RegisterType((*SyncStartResponse)(nil), "protos.SyncStartResponse")
	proto.RegisterType((*UpdatedValue)(nil), "protos.UpdatedValue")
	proto.RegisterType((*ChaincodeStateDelta)(nil), "protos.ChaincodeStateDelta")
	proto.RegisterType((*SyncStateChunk)(nil), "protos.SyncStateChunk")
	proto.RegisterType((*SyncMessage)(nil), "protos.SyncMessage")
	proto.RegisterType((*SyncState)(nil), "protos.SyncState")
	proto.RegisterType((*SyncOffset)(nil), "protos.SyncOffset")
	proto.RegisterType((*BucketTreeOffset)(nil), "protos.BucketTreeOffset")
	proto.RegisterType((*BlockOffset)(nil), "protos.BlockOffset")
	proto.RegisterEnum("protos.SyncType", SyncType_name, SyncType_value)
	proto.RegisterEnum("protos.SyncMsg_Type", SyncMsg_Type_name, SyncMsg_Type_value)
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// Client API for Sync service

type SyncClient interface {
	// response for the controlling channel: state searching and info
	Control(ctx context.Context, opts ...grpc.CallOption) (Sync_ControlClient, error)
	Data(ctx context.Context, opts ...grpc.CallOption) (Sync_DataClient, error)
}

type syncClient struct {
	cc *grpc.ClientConn
}

func NewSyncClient(cc *grpc.ClientConn) SyncClient {
	return &syncClient{cc}
}

func (c *syncClient) Control(ctx context.Context, opts ...grpc.CallOption) (Sync_ControlClient, error) {
	stream, err := grpc.NewClientStream(ctx, &_Sync_serviceDesc.Streams[0], c.cc, "/protos.Sync/Control", opts...)
	if err != nil {
		return nil, err
	}
	x := &syncControlClient{stream}
	return x, nil
}

type Sync_ControlClient interface {
	Send(*SyncMsg) error
	Recv() (*SyncMsg, error)
	grpc.ClientStream
}

type syncControlClient struct {
	grpc.ClientStream
}

func (x *syncControlClient) Send(m *SyncMsg) error {
	return x.ClientStream.SendMsg(m)
}

func (x *syncControlClient) Recv() (*SyncMsg, error) {
	m := new(SyncMsg)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *syncClient) Data(ctx context.Context, opts ...grpc.CallOption) (Sync_DataClient, error) {
	stream, err := grpc.NewClientStream(ctx, &_Sync_serviceDesc.Streams[1], c.cc, "/protos.Sync/Data", opts...)
	if err != nil {
		return nil, err
	}
	x := &syncDataClient{stream}
	return x, nil
}

type Sync_DataClient interface {
	Send(*SyncMsg) error
	Recv() (*SyncMsg, error)
	grpc.ClientStream
}

type syncDataClient struct {
	grpc.ClientStream
}

func (x *syncDataClient) Send(m *SyncMsg) error {
	return x.ClientStream.SendMsg(m)
}

func (x *syncDataClient) Recv() (*SyncMsg, error) {
	m := new(SyncMsg)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// Server API for Sync service

type SyncServer interface {
	// response for the controlling channel: state searching and info
	Control(Sync_ControlServer) error
	Data(Sync_DataServer) error
}

func RegisterSyncServer(s *grpc.Server, srv SyncServer) {
	s.RegisterService(&_Sync_serviceDesc, srv)
}

func _Sync_Control_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(SyncServer).Control(&syncControlServer{stream})
}

type Sync_ControlServer interface {
	Send(*SyncMsg) error
	Recv() (*SyncMsg, error)
	grpc.ServerStream
}

type syncControlServer struct {
	grpc.ServerStream
}

func (x *syncControlServer) Send(m *SyncMsg) error {
	return x.ServerStream.SendMsg(m)
}

func (x *syncControlServer) Recv() (*SyncMsg, error) {
	m := new(SyncMsg)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func _Sync_Data_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(SyncServer).Data(&syncDataServer{stream})
}

type Sync_DataServer interface {
	Send(*SyncMsg) error
	Recv() (*SyncMsg, error)
	grpc.ServerStream
}

type syncDataServer struct {
	grpc.ServerStream
}

func (x *syncDataServer) Send(m *SyncMsg) error {
	return x.ServerStream.SendMsg(m)
}

func (x *syncDataServer) Recv() (*SyncMsg, error) {
	m := new(SyncMsg)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

var _Sync_serviceDesc = grpc.ServiceDesc{
	ServiceName: "protos.Sync",
	HandlerType: (*SyncServer)(nil),
	Methods:     []grpc.MethodDesc{},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "Control",
			Handler:       _Sync_Control_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
		{
			StreamName:    "Data",
			Handler:       _Sync_Data_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
	Metadata: "sync.proto",
}

func init() { proto.RegisterFile("sync.proto", fileDescriptor8) }

var fileDescriptor8 = []byte{
	// 1191 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0xa4, 0x57, 0xdb, 0x6e, 0xdb, 0x46,
	0x13, 0x0e, 0xa9, 0x83, 0xed, 0xd1, 0xc1, 0xf4, 0xda, 0xbf, 0xac, 0x28, 0x7f, 0x0a, 0x95, 0x4d,
	0x0b, 0xc3, 0x0d, 0x04, 0x47, 0xbd, 0x09, 0x72, 0x55, 0x5b, 0x92, 0x13, 0xd7, 0xb1, 0x94, 0x50,
	0x72, 0x0a, 0x03, 0x45, 0x8d, 0x95, 0xb8, 0xb6, 0x54, 0x33, 0xa4, 0xca, 0x5d, 0x19, 0xd5, 0x33,
	0xf4, 0x31, 0xda, 0xcb, 0x5e, 0xb7, 0xcf, 0xd1, 0xa7, 0xe9, 0x6d, 0xb1, 0xb3, 0x14, 0xc5, 0x83,
	0x9c, 0xc4, 0xe8, 0x95, 0x38, 0xe7, 0x9d, 0x6f, 0x66, 0x67, 0x56, 0x00, 0x7c, 0xee, 0x8e, 0x1a,
	0x53, 0xdf, 0x13, 0x1e, 0xc9, 0xe3, 0x0f, 0xaf, 0x15, 0xaf, 0xe8, 0xd0, 0x9f, 0x04, 0x5c, 0xf3,
	0x1f, 0x0d, 0x0a, 0x2f, 0x1d, 0x6f, 0x48, 0x9d, 0xbe, 0xa0, 0x82, 0x91, 0x1d, 0xc8, 0x8d, 0xbc,
	0x99, 0x2b, 0xaa, 0x5a, 0x5d, 0xdb, 0xcb, 0x5a, 0x8a, 0x20, 0x4f, 0x61, 0xcb, 0x65, 0xbf, 0x88,
	0xae, 0x67, 0x33, 0x54, 0x7b, 0x45, 0xf9, 0xb8, 0x9a, 0xa9, 0x67, 0xf6, 0x8a, 0x56, 0x5a, 0x40,
	0x0e, 0x60, 0x7b, 0x4a, 0x7d, 0xe6, 0x26, 0xf4, 0xb3, 0xa8, 0xbf, 0x4a, 0x44, 0x9e, 0xc3, 0xae,
	0x43, 0xb9, 0x38, 0xf2, 0xa9, 0x3b, 0x1a, 0xc7, 0xad, 0x72, 0x75, 0x6d, 0xaf, 0x68, 0xdd, 0x25,
	0x96, 0x96, 0xf2, 0x00, 0xab, 0x2c, 0xd7, 0x94, 0xe5, 0x1d, 0x62, 0x73, 0x02, 0x70, 0xe4, 0x78,
	0xa3, 0x1b, 0x95, 0xf7, 0x17, 0x90, 0x1b, 0x4a, 0x0a, 0xf3, 0x2e, 0x34, 0x4b, 0x0a, 0x1e, 0xde,
	0x40, 0x15, 0x4b, 0xc9, 0xc8, 0x67, 0x00, 0x5c, 0x6a, 0xb7, 0x99, 0x23, 0x68, 0x55, 0x47, 0xff,
	0x11, 0x0e, 0xa9, 0x40, 0xfe, 0x15, 0x9b, 0x5c, 0x8f, 0x45, 0x35, 0x83, 0xe8, 0x05, 0x94, 0xf9,
	0x23, 0x94, 0xfb, 0x73, 0x77, 0xa4, 0x7c, 0x51, 0xf7, 0x9a, 0x91, 0x27, 0x50, 0x1a, 0x79, 0xbe,
	0xcf, 0x1c, 0x2a, 0x26, 0x9e, 0x7b, 0x62, 0x07, 0x70, 0xc7, 0x99, 0xb2, 0x18, 0x5c, 0x50, 0x5f,
	0x60, 0xa8, 0xac, 0xa5, 0x08, 0x62, 0x40, 0x86, 0xb9, 0x76, 0x10, 0x42, 0x7e, 0x9a, 0x14, 0x20,
	0xf4, 0xcf, 0xc9, 0x53, 0xc8, 0xf9, 0x32, 0x48, 0x90, 0x4a, 0x65, 0x91, 0x4a, 0xfc, 0x08, 0x96,
	0x52, 0x22, 0x5f, 0x42, 0x1e, 0x93, 0xe3, 0x55, 0xbd, 0x9e, 0x49, 0x67, 0x1e, 0x08, 0xcd, 0x6f,
	0xa1, 0x2a, 0xed, 0x11, 0xac, 0xbe, 0x4b, 0xa7, 0x7c, 0xec, 0x09, 0x8b, 0xfd, 0x3c, 0x63, 0x5c,
	0x7c, 0x5a, 0x32, 0xe6, 0x6f, 0x1a, 0x6c, 0xa5, 0x5c, 0xc8, 0x14, 0x6d, 0x44, 0x53, 0x43, 0x34,
	0x15, 0x41, 0x6a, 0xb0, 0xce, 0xa5, 0x73, 0x77, 0xc4, 0x82, 0xdc, 0x43, 0x9a, 0xd4, 0xa1, 0x80,
	0x67, 0xea, 0xce, 0xde, 0x0f, 0x99, 0x1f, 0xc0, 0x10, 0x65, 0x91, 0x17, 0xb0, 0xe6, 0xab, 0xa3,
	0x55, 0xb3, 0x08, 0x41, 0x3d, 0x0a, 0xc1, 0xaa, 0x14, 0xac, 0x85, 0x81, 0x79, 0x0c, 0x95, 0x50,
	0x09, 0x8b, 0xca, 0x17, 0x59, 0xde, 0x0b, 0x56, 0xf3, 0x7b, 0xd8, 0x4c, 0xf8, 0xb9, 0x67, 0x5d,
	0x2a, 0x90, 0x47, 0x2c, 0x54, 0x5d, 0x8a, 0x56, 0x40, 0x99, 0x6e, 0xa4, 0x97, 0x54, 0xeb, 0xde,
	0xcf, 0x6f, 0x03, 0xd6, 0xe5, 0x50, 0xb0, 0x29, 0x76, 0xb0, 0xac, 0x38, 0x89, 0x55, 0x1c, 0x7d,
	0x5a, 0xa1, 0x8e, 0xf9, 0x47, 0x06, 0xd6, 0xa4, 0xa7, 0x33, 0x7e, 0x4d, 0xf6, 0x20, 0x2b, 0xe6,
	0x53, 0x15, 0xa8, 0xdc, 0xdc, 0x89, 0x06, 0x3a, 0xe3, 0xd7, 0x8d, 0xc1, 0x7c, 0xca, 0x2c, 0xd4,
	0x48, 0xb7, 0x84, 0xbe, 0xaa, 0xbf, 0xab, 0xb0, 0x36, 0xa5, 0x73, 0xc7, 0xa3, 0xaa, 0x9b, 0x8b,
	0xd6, 0x82, 0x34, 0xff, 0xd4, 0x21, 0x2b, 0xdd, 0x91, 0x12, 0x6c, 0x9c, 0x77, 0xdb, 0x9d, 0xe3,
	0x93, 0x6e, 0xa7, 0x6d, 0x3c, 0x20, 0xff, 0x83, 0xad, 0xfe, 0x45, 0xb7, 0x75, 0xd9, 0x1f, 0x1c,
	0x0e, 0x3a, 0x97, 0xdd, 0xde, 0xe0, 0xe4, 0xf8, 0xc2, 0xd0, 0x08, 0x81, 0x72, 0x84, 0xdd, 0x7b,
	0x33, 0x30, 0x74, 0x52, 0x01, 0xa2, 0x78, 0x9d, 0x7e, 0xff, 0xa4, 0xd7, 0x95, 0x32, 0x6b, 0x60,
	0x64, 0x48, 0x0d, 0x2a, 0x69, 0xfe, 0xe5, 0x61, 0xeb, 0xd4, 0xc8, 0xa6, 0x6c, 0xde, 0x9e, 0x77,
	0xac, 0x0b, 0x23, 0x97, 0xb2, 0x41, 0x3e, 0xda, 0xe4, 0xc9, 0x0e, 0x18, 0x31, 0x59, 0xa7, 0xdb,
	0x36, 0xd6, 0xc9, 0x2e, 0x6c, 0xc7, 0xb8, 0xed, 0xce, 0xeb, 0xc1, 0x61, 0xdf, 0x30, 0xc8, 0x23,
	0xd8, 0x5d, 0x21, 0x40, 0x5f, 0x5b, 0xe4, 0x31, 0x3c, 0x8c, 0x9f, 0x4d, 0x12, 0x67, 0x9d, 0x7e,
	0xff, 0xf0, 0x65, 0xc7, 0x20, 0xe4, 0x73, 0x78, 0x7c, 0xa7, 0x18, 0x3d, 0x6c, 0x9b, 0x47, 0xaa,
	0x3d, 0xb0, 0x8a, 0x6f, 0x67, 0xcc, 0x9f, 0x93, 0x32, 0xe8, 0x13, 0x75, 0x25, 0x4b, 0x96, 0x3e,
	0xb1, 0xc3, 0xfb, 0x13, 0x4c, 0x2a, 0x3d, 0x72, 0x7f, 0x82, 0x71, 0x35, 0x87, 0x52, 0xe8, 0xc3,
	0x62, 0x7c, 0x9a, 0x72, 0x21, 0xeb, 0xe6, 0x71, 0x31, 0xb9, 0x65, 0x58, 0xb7, 0x75, 0x6b, 0x41,
	0x92, 0xff, 0xc3, 0x06, 0xce, 0xc3, 0xb1, 0x1a, 0xf8, 0xb2, 0xa6, 0x4b, 0x46, 0x32, 0x74, 0x2e,
	0x1d, 0x7a, 0x0a, 0x46, 0x10, 0xda, 0x0f, 0xc7, 0x4b, 0x32, 0x7a, 0x13, 0x0a, 0x41, 0x9b, 0xc8,
	0x0e, 0xc1, 0x04, 0xca, 0x4d, 0x23, 0xda, 0x8c, 0xd8, 0x88, 0x51, 0xa5, 0x0f, 0x74, 0xda, 0xaf,
	0xcb, 0xb1, 0x24, 0x43, 0xf2, 0xa9, 0xe7, 0x72, 0x96, 0x8a, 0xf9, 0x15, 0x94, 0x7d, 0xf6, 0x13,
	0x1b, 0x09, 0x66, 0x5b, 0x8c, 0x72, 0xcf, 0xc5, 0xb0, 0x1b, 0x56, 0x82, 0xfb, 0x9f, 0xf3, 0xff,
	0x0e, 0x8a, 0xe7, 0x53, 0x9b, 0x0a, 0x66, 0xbf, 0xa3, 0xce, 0x0c, 0xd7, 0x31, 0x7e, 0x2c, 0xc6,
	0xa3, 0xe2, 0x3e, 0x81, 0xd2, 0x1b, 0x9f, 0xdd, 0x4e, 0xbc, 0x19, 0x57, 0x52, 0xb5, 0x8a, 0xe2,
	0x4c, 0xf3, 0x2f, 0x0d, 0xb6, 0x5b, 0x63, 0x3a, 0x71, 0x47, 0x8b, 0xbd, 0xa7, 0xb6, 0xd4, 0x29,
	0x40, 0x10, 0xe3, 0xf4, 0x1d, 0xaf, 0x6a, 0x38, 0x03, 0xbe, 0x5e, 0xc0, 0xb7, 0xc2, 0xa0, 0xb1,
	0xd4, 0xee, 0xb8, 0xc2, 0x9f, 0x5b, 0x11, 0xf3, 0x5a, 0x1f, 0x36, 0x13, 0x62, 0xb9, 0x9f, 0x6e,
	0xd8, 0x1c, 0x4f, 0xbc, 0x61, 0xc9, 0x4f, 0xb2, 0x0f, 0xb9, 0xdb, 0xf0, 0x9c, 0x85, 0xe5, 0xe0,
	0x88, 0xa6, 0x6a, 0x29, 0x95, 0x17, 0xfa, 0x73, 0xcd, 0xfc, 0x5d, 0x8f, 0x74, 0x71, 0x6b, 0x3c,
	0x73, 0x6f, 0xc8, 0x3e, 0xe4, 0xbd, 0xab, 0x2b, 0xce, 0x44, 0x30, 0xe5, 0x48, 0xb4, 0xde, 0x3d,
	0x94, 0x58, 0x81, 0x86, 0xdc, 0x1e, 0xbe, 0xe7, 0x09, 0xac, 0x81, 0x42, 0x26, 0xa4, 0x89, 0x0d,
	0x3b, 0x2b, 0x52, 0xe4, 0xf8, 0x98, 0x29, 0x34, 0x0f, 0x52, 0x8b, 0x02, 0xa3, 0xaf, 0x42, 0x25,
	0xc0, 0x62, 0xa5, 0xb7, 0x9a, 0x0d, 0x0f, 0xef, 0x34, 0x59, 0x81, 0xcf, 0xb3, 0x38, 0x3e, 0x8f,
	0x3e, 0x50, 0x8c, 0x28, 0x4c, 0x7f, 0x6b, 0x50, 0xc0, 0xd9, 0xcb, 0x38, 0xa7, 0xd7, 0x2c, 0x79,
	0x31, 0xb4, 0x7b, 0x5e, 0x0c, 0x3d, 0x76, 0x31, 0x22, 0x88, 0x67, 0x3e, 0x8a, 0x78, 0x6a, 0xdc,
	0x67, 0x57, 0x8d, 0x7b, 0x13, 0x8a, 0x57, 0x74, 0xe2, 0x84, 0x57, 0x28, 0x87, 0x08, 0xc4, 0x78,
	0xe6, 0x39, 0x6c, 0x84, 0xd8, 0xc7, 0x6f, 0x93, 0x96, 0xbc, 0x4d, 0xcb, 0x03, 0xea, 0x1f, 0x3b,
	0xa0, 0x59, 0x57, 0x2f, 0x24, 0xc5, 0x25, 0x04, 0xb2, 0xb8, 0xff, 0x94, 0x4b, 0xfc, 0x36, 0x7f,
	0x00, 0xe3, 0x68, 0x36, 0xba, 0x61, 0x62, 0xe0, 0x33, 0x16, 0xe8, 0xed, 0x40, 0xce, 0x61, 0xb7,
	0xcc, 0x59, 0x3c, 0x86, 0x91, 0x90, 0xa7, 0x1a, 0xa2, 0x66, 0x77, 0xf6, 0x3e, 0x18, 0x9f, 0x4b,
	0xc6, 0xf2, 0x41, 0xa3, 0x1e, 0x26, 0x8a, 0x30, 0x0f, 0xa1, 0x80, 0xdb, 0xb5, 0x17, 0x76, 0x28,
	0xbe, 0xe5, 0xa4, 0x07, 0x2d, 0x78, 0xdf, 0x04, 0xb4, 0x5c, 0xfc, 0xcc, 0xb5, 0x97, 0xbe, 0x03,
	0x6a, 0x9f, 0xc2, 0xfa, 0xa2, 0x84, 0xc9, 0xad, 0x58, 0x06, 0xc0, 0x55, 0x70, 0xf4, 0xba, 0xd7,
	0x3a, 0x8d, 0xac, 0x43, 0xa4, 0x71, 0x31, 0xe8, 0xa1, 0x0e, 0xae, 0x48, 0x23, 0x93, 0x58, 0x99,
	0xb8, 0xfe, 0x9a, 0x13, 0xc8, 0xca, 0x10, 0xe4, 0x19, 0xac, 0xb5, 0x3c, 0x57, 0xf8, 0x9e, 0x43,
	0x36, 0x13, 0x4b, 0xbe, 0x96, 0x64, 0x98, 0x0f, 0xf6, 0xb4, 0x03, 0x8d, 0x34, 0x20, 0xdb, 0xa6,
	0x82, 0x7e, 0xaa, 0xfe, 0x50, 0xfd, 0x1b, 0xf9, 0xe6, 0xdf, 0x00, 0x00, 0x00, 0xff, 0xff, 0x0e,
	0xb0, 0x06, 0xc6, 0xa2, 0x0c, 0x00, 0x00,
}
