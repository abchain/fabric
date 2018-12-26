// Code generated by protoc-gen-go. DO NOT EDIT.
// source: api.proto

/*
Package protos is a generated protocol buffer package.

It is generated from these files:
	api.proto
	chaincode.proto
	chaincodeevent.proto
	devops.proto
	events.proto
	fabric.proto
	gossip.proto
	server_admin.proto
	sync.proto

It has these top-level messages:
	BlockNumber
	BlockCount
	ChaincodeID
	ChaincodeInput
	ChaincodeSpec
	ChaincodeDeploymentSpec
	ChaincodeInvocationSpec
	ChaincodeSecurityContext
	ChaincodeMessage
	PutStateInfo
	RangeQueryState
	RangeQueryStateNext
	RangeQueryStateClose
	RangeQueryStateKeyValue
	RangeQueryStateResponse
	ChaincodeEvent
	Secret
	SigmaInput
	ExecuteWithBinding
	SigmaOutput
	BuildResult
	TransactionRequest
	ChaincodeReg
	Interest
	Register
	Rejection
	Unregister
	Event
	Transaction
	TransactionBlock
	TransactionResult
	Block
	BlockchainInfo
	NonHashData
	PeerAddress
	PeerID
	PeerEndpoint
	PeersMessage
	PeersAddresses
	HelloMessage
	Message
	Response
	GossipMsg
	HotTransactionBlock
	PeerTxState
	Gossip_Tx
	Gossip_TxState
	ServerStatus
	GlobalState
	BlockState
	SyncBlockRange
	SyncBlocks
	SyncStateSnapshotRequest
	SyncStateSnapshot
	SyncStateDeltasRequest
	SyncStateDeltas
	SyncBlockState
	SyncMsg
	SyncStateQuery
	SyncStateResp
	SyncStartRequest
	SyncStartResponse
	UpdatedValue
	ChaincodeStateDelta
	SyncStateChunk
	SyncMessage
	SyncState
	StateOffset
	BucketTreeOffset
*/
package protos

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"
import google_protobuf1 "github.com/golang/protobuf/ptypes/empty"

import (
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

// Specifies the block number to be returned from the blockchain.
type BlockNumber struct {
	Number uint64 `protobuf:"varint,1,opt,name=number" json:"number,omitempty"`
}

func (m *BlockNumber) Reset()                    { *m = BlockNumber{} }
func (m *BlockNumber) String() string            { return proto.CompactTextString(m) }
func (*BlockNumber) ProtoMessage()               {}
func (*BlockNumber) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{0} }

func (m *BlockNumber) GetNumber() uint64 {
	if m != nil {
		return m.Number
	}
	return 0
}

// Specifies the current number of blocks in the blockchain.
type BlockCount struct {
	Count uint64 `protobuf:"varint,1,opt,name=count" json:"count,omitempty"`
}

func (m *BlockCount) Reset()                    { *m = BlockCount{} }
func (m *BlockCount) String() string            { return proto.CompactTextString(m) }
func (*BlockCount) ProtoMessage()               {}
func (*BlockCount) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{1} }

func (m *BlockCount) GetCount() uint64 {
	if m != nil {
		return m.Count
	}
	return 0
}

func init() {
	proto.RegisterType((*BlockNumber)(nil), "protos.BlockNumber")
	proto.RegisterType((*BlockCount)(nil), "protos.BlockCount")
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// Client API for Openchain service

type OpenchainClient interface {
	// GetBlockchainInfo returns information about the blockchain ledger such as
	// height, current block hash, and previous block hash.
	GetBlockchainInfo(ctx context.Context, in *google_protobuf1.Empty, opts ...grpc.CallOption) (*BlockchainInfo, error)
	// GetBlockByNumber returns the data contained within a specific block in the
	// blockchain. The genesis block is block zero.
	GetBlockByNumber(ctx context.Context, in *BlockNumber, opts ...grpc.CallOption) (*Block, error)
	// GetBlockCount returns the current number of blocks in the blockchain data
	// structure.
	GetBlockCount(ctx context.Context, in *google_protobuf1.Empty, opts ...grpc.CallOption) (*BlockCount, error)
	// GetPeers returns a list of all peer nodes currently connected to the target
	// peer.
	GetPeers(ctx context.Context, in *google_protobuf1.Empty, opts ...grpc.CallOption) (*PeersMessage, error)
	// GetPeerEndpoint returns self peer node
	GetPeerEndpoint(ctx context.Context, in *google_protobuf1.Empty, opts ...grpc.CallOption) (*PeersMessage, error)
}

type openchainClient struct {
	cc *grpc.ClientConn
}

func NewOpenchainClient(cc *grpc.ClientConn) OpenchainClient {
	return &openchainClient{cc}
}

func (c *openchainClient) GetBlockchainInfo(ctx context.Context, in *google_protobuf1.Empty, opts ...grpc.CallOption) (*BlockchainInfo, error) {
	out := new(BlockchainInfo)
	err := grpc.Invoke(ctx, "/protos.Openchain/GetBlockchainInfo", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *openchainClient) GetBlockByNumber(ctx context.Context, in *BlockNumber, opts ...grpc.CallOption) (*Block, error) {
	out := new(Block)
	err := grpc.Invoke(ctx, "/protos.Openchain/GetBlockByNumber", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *openchainClient) GetBlockCount(ctx context.Context, in *google_protobuf1.Empty, opts ...grpc.CallOption) (*BlockCount, error) {
	out := new(BlockCount)
	err := grpc.Invoke(ctx, "/protos.Openchain/GetBlockCount", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *openchainClient) GetPeers(ctx context.Context, in *google_protobuf1.Empty, opts ...grpc.CallOption) (*PeersMessage, error) {
	out := new(PeersMessage)
	err := grpc.Invoke(ctx, "/protos.Openchain/GetPeers", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *openchainClient) GetPeerEndpoint(ctx context.Context, in *google_protobuf1.Empty, opts ...grpc.CallOption) (*PeersMessage, error) {
	out := new(PeersMessage)
	err := grpc.Invoke(ctx, "/protos.Openchain/GetPeerEndpoint", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Server API for Openchain service

type OpenchainServer interface {
	// GetBlockchainInfo returns information about the blockchain ledger such as
	// height, current block hash, and previous block hash.
	GetBlockchainInfo(context.Context, *google_protobuf1.Empty) (*BlockchainInfo, error)
	// GetBlockByNumber returns the data contained within a specific block in the
	// blockchain. The genesis block is block zero.
	GetBlockByNumber(context.Context, *BlockNumber) (*Block, error)
	// GetBlockCount returns the current number of blocks in the blockchain data
	// structure.
	GetBlockCount(context.Context, *google_protobuf1.Empty) (*BlockCount, error)
	// GetPeers returns a list of all peer nodes currently connected to the target
	// peer.
	GetPeers(context.Context, *google_protobuf1.Empty) (*PeersMessage, error)
	// GetPeerEndpoint returns self peer node
	GetPeerEndpoint(context.Context, *google_protobuf1.Empty) (*PeersMessage, error)
}

func RegisterOpenchainServer(s *grpc.Server, srv OpenchainServer) {
	s.RegisterService(&_Openchain_serviceDesc, srv)
}

func _Openchain_GetBlockchainInfo_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(google_protobuf1.Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(OpenchainServer).GetBlockchainInfo(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/protos.Openchain/GetBlockchainInfo",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(OpenchainServer).GetBlockchainInfo(ctx, req.(*google_protobuf1.Empty))
	}
	return interceptor(ctx, in, info, handler)
}

func _Openchain_GetBlockByNumber_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(BlockNumber)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(OpenchainServer).GetBlockByNumber(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/protos.Openchain/GetBlockByNumber",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(OpenchainServer).GetBlockByNumber(ctx, req.(*BlockNumber))
	}
	return interceptor(ctx, in, info, handler)
}

func _Openchain_GetBlockCount_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(google_protobuf1.Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(OpenchainServer).GetBlockCount(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/protos.Openchain/GetBlockCount",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(OpenchainServer).GetBlockCount(ctx, req.(*google_protobuf1.Empty))
	}
	return interceptor(ctx, in, info, handler)
}

func _Openchain_GetPeers_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(google_protobuf1.Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(OpenchainServer).GetPeers(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/protos.Openchain/GetPeers",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(OpenchainServer).GetPeers(ctx, req.(*google_protobuf1.Empty))
	}
	return interceptor(ctx, in, info, handler)
}

func _Openchain_GetPeerEndpoint_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(google_protobuf1.Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(OpenchainServer).GetPeerEndpoint(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/protos.Openchain/GetPeerEndpoint",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(OpenchainServer).GetPeerEndpoint(ctx, req.(*google_protobuf1.Empty))
	}
	return interceptor(ctx, in, info, handler)
}

var _Openchain_serviceDesc = grpc.ServiceDesc{
	ServiceName: "protos.Openchain",
	HandlerType: (*OpenchainServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "GetBlockchainInfo",
			Handler:    _Openchain_GetBlockchainInfo_Handler,
		},
		{
			MethodName: "GetBlockByNumber",
			Handler:    _Openchain_GetBlockByNumber_Handler,
		},
		{
			MethodName: "GetBlockCount",
			Handler:    _Openchain_GetBlockCount_Handler,
		},
		{
			MethodName: "GetPeers",
			Handler:    _Openchain_GetPeers_Handler,
		},
		{
			MethodName: "GetPeerEndpoint",
			Handler:    _Openchain_GetPeerEndpoint_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api.proto",
}

func init() { proto.RegisterFile("api.proto", fileDescriptor0) }

var fileDescriptor0 = []byte{
	// 262 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x7c, 0x91, 0x4f, 0x4b, 0xc3, 0x40,
	0x10, 0xc5, 0x53, 0xd1, 0x62, 0x47, 0x8b, 0x3a, 0x96, 0x20, 0xf1, 0x22, 0x0b, 0x82, 0xa7, 0x2d,
	0xe8, 0x45, 0x04, 0x0f, 0x56, 0x42, 0xf1, 0xe0, 0x1f, 0xfc, 0x06, 0x49, 0x9c, 0xc4, 0x60, 0xbb,
	0xbb, 0x64, 0x37, 0x87, 0x7e, 0x45, 0x3f, 0x95, 0x64, 0x27, 0x39, 0xec, 0x21, 0x9e, 0x32, 0xef,
	0xe5, 0xfd, 0x66, 0x1e, 0x2c, 0xcc, 0x32, 0x53, 0x4b, 0xd3, 0x68, 0xa7, 0x71, 0xea, 0x3f, 0x36,
	0x39, 0x2e, 0xb3, 0xbc, 0xa9, 0x0b, 0x76, 0x93, 0xcb, 0x4a, 0xeb, 0x6a, 0x43, 0x4b, 0xaf, 0xf2,
	0xb6, 0x5c, 0xd2, 0xd6, 0xb8, 0x1d, 0xff, 0x14, 0xd7, 0x70, 0xb4, 0xda, 0xe8, 0xe2, 0xe7, 0xad,
	0xdd, 0xe6, 0xd4, 0x60, 0x0c, 0x53, 0xe5, 0xa7, 0x8b, 0xc9, 0xd5, 0xe4, 0x66, 0xff, 0xb3, 0x57,
	0x42, 0x00, 0xf8, 0xd8, 0xb3, 0x6e, 0x95, 0xc3, 0x05, 0x1c, 0x14, 0xdd, 0xd0, 0x87, 0x58, 0xdc,
	0xfe, 0xee, 0xc1, 0xec, 0xdd, 0x90, 0x2a, 0xbe, 0xb3, 0x5a, 0x61, 0x0a, 0x67, 0x6b, 0x72, 0x1e,
	0xf2, 0xc6, 0x8b, 0x2a, 0x35, 0xc6, 0x92, 0xbb, 0xc8, 0xa1, 0x8b, 0x4c, 0xbb, 0x2e, 0x49, 0xcc,
	0x86, 0x95, 0x61, 0x5e, 0x44, 0x78, 0x0f, 0xa7, 0xc3, 0x9a, 0xd5, 0xae, 0x2f, 0x79, 0x1e, 0xa4,
	0xd9, 0x4c, 0xe6, 0x81, 0x29, 0x22, 0x7c, 0x84, 0xf9, 0x40, 0x72, 0xeb, 0xb1, 0xe3, 0x18, 0x90,
	0x3e, 0x2b, 0x22, 0x7c, 0x80, 0xc3, 0x35, 0xb9, 0x0f, 0xa2, 0xc6, 0x8e, 0x92, 0x8b, 0x81, 0xf4,
	0xb1, 0x57, 0xb2, 0x36, 0xab, 0x48, 0x44, 0xf8, 0x04, 0x27, 0x3d, 0x9b, 0xaa, 0x2f, 0xa3, 0xeb,
	0x7f, 0x8e, 0x8f, 0xac, 0xc8, 0xf9, 0x29, 0xef, 0xfe, 0x02, 0x00, 0x00, 0xff, 0xff, 0x3b, 0x5a,
	0xdb, 0xfb, 0xde, 0x01, 0x00, 0x00,
}
