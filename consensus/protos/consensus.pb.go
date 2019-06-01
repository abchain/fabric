// Code generated by protoc-gen-go. DO NOT EDIT.
// source: consensus.proto

/*
Package csprotos is a generated protocol buffer package.

It is generated from these files:
	consensus.proto

It has these top-level messages:
	ConsensusPurpose
	PurposeBlock
	PurposeTransactions
	PurposeBlocks
	ConsensusOutput
*/
package csprotos

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"
import protos2 "github.com/abchain/fabric/protos"
import google_protobuf1 "github.com/golang/protobuf/ptypes/empty"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

type ConsensusPurpose struct {
	// Types that are valid to be assigned to Out:
	//	*ConsensusPurpose_Txs
	//	*ConsensusPurpose_Nothing
	//	*ConsensusPurpose_Error
	Out isConsensusPurpose_Out `protobuf_oneof:"out"`
}

func (m *ConsensusPurpose) Reset()                    { *m = ConsensusPurpose{} }
func (m *ConsensusPurpose) String() string            { return proto.CompactTextString(m) }
func (*ConsensusPurpose) ProtoMessage()               {}
func (*ConsensusPurpose) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{0} }

type isConsensusPurpose_Out interface {
	isConsensusPurpose_Out()
}

type ConsensusPurpose_Txs struct {
	Txs *protos2.TransactionBlock `protobuf:"bytes,1,opt,name=txs,oneof"`
}
type ConsensusPurpose_Nothing struct {
	Nothing *google_protobuf1.Empty `protobuf:"bytes,2,opt,name=nothing,oneof"`
}
type ConsensusPurpose_Error struct {
	Error string `protobuf:"bytes,3,opt,name=error,oneof"`
}

func (*ConsensusPurpose_Txs) isConsensusPurpose_Out()     {}
func (*ConsensusPurpose_Nothing) isConsensusPurpose_Out() {}
func (*ConsensusPurpose_Error) isConsensusPurpose_Out()   {}

func (m *ConsensusPurpose) GetOut() isConsensusPurpose_Out {
	if m != nil {
		return m.Out
	}
	return nil
}

func (m *ConsensusPurpose) GetTxs() *protos2.TransactionBlock {
	if x, ok := m.GetOut().(*ConsensusPurpose_Txs); ok {
		return x.Txs
	}
	return nil
}

func (m *ConsensusPurpose) GetNothing() *google_protobuf1.Empty {
	if x, ok := m.GetOut().(*ConsensusPurpose_Nothing); ok {
		return x.Nothing
	}
	return nil
}

func (m *ConsensusPurpose) GetError() string {
	if x, ok := m.GetOut().(*ConsensusPurpose_Error); ok {
		return x.Error
	}
	return ""
}

// XXX_OneofFuncs is for the internal use of the proto package.
func (*ConsensusPurpose) XXX_OneofFuncs() (func(msg proto.Message, b *proto.Buffer) error, func(msg proto.Message, tag, wire int, b *proto.Buffer) (bool, error), func(msg proto.Message) (n int), []interface{}) {
	return _ConsensusPurpose_OneofMarshaler, _ConsensusPurpose_OneofUnmarshaler, _ConsensusPurpose_OneofSizer, []interface{}{
		(*ConsensusPurpose_Txs)(nil),
		(*ConsensusPurpose_Nothing)(nil),
		(*ConsensusPurpose_Error)(nil),
	}
}

func _ConsensusPurpose_OneofMarshaler(msg proto.Message, b *proto.Buffer) error {
	m := msg.(*ConsensusPurpose)
	// out
	switch x := m.Out.(type) {
	case *ConsensusPurpose_Txs:
		b.EncodeVarint(1<<3 | proto.WireBytes)
		if err := b.EncodeMessage(x.Txs); err != nil {
			return err
		}
	case *ConsensusPurpose_Nothing:
		b.EncodeVarint(2<<3 | proto.WireBytes)
		if err := b.EncodeMessage(x.Nothing); err != nil {
			return err
		}
	case *ConsensusPurpose_Error:
		b.EncodeVarint(3<<3 | proto.WireBytes)
		b.EncodeStringBytes(x.Error)
	case nil:
	default:
		return fmt.Errorf("ConsensusPurpose.Out has unexpected type %T", x)
	}
	return nil
}

func _ConsensusPurpose_OneofUnmarshaler(msg proto.Message, tag, wire int, b *proto.Buffer) (bool, error) {
	m := msg.(*ConsensusPurpose)
	switch tag {
	case 1: // out.txs
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		msg := new(protos2.TransactionBlock)
		err := b.DecodeMessage(msg)
		m.Out = &ConsensusPurpose_Txs{msg}
		return true, err
	case 2: // out.nothing
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		msg := new(google_protobuf1.Empty)
		err := b.DecodeMessage(msg)
		m.Out = &ConsensusPurpose_Nothing{msg}
		return true, err
	case 3: // out.error
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		x, err := b.DecodeStringBytes()
		m.Out = &ConsensusPurpose_Error{x}
		return true, err
	default:
		return false, nil
	}
}

func _ConsensusPurpose_OneofSizer(msg proto.Message) (n int) {
	m := msg.(*ConsensusPurpose)
	// out
	switch x := m.Out.(type) {
	case *ConsensusPurpose_Txs:
		s := proto.Size(x.Txs)
		n += proto.SizeVarint(1<<3 | proto.WireBytes)
		n += proto.SizeVarint(uint64(s))
		n += s
	case *ConsensusPurpose_Nothing:
		s := proto.Size(x.Nothing)
		n += proto.SizeVarint(2<<3 | proto.WireBytes)
		n += proto.SizeVarint(uint64(s))
		n += s
	case *ConsensusPurpose_Error:
		n += proto.SizeVarint(3<<3 | proto.WireBytes)
		n += proto.SizeVarint(uint64(len(x.Error)))
		n += len(x.Error)
	case nil:
	default:
		panic(fmt.Sprintf("proto: unexpected type %T in oneof", x))
	}
	return n
}

type PurposeBlock struct {
	N uint64         `protobuf:"varint,1,opt,name=n" json:"n,omitempty"`
	B *protos2.Block `protobuf:"bytes,2,opt,name=b" json:"b,omitempty"`
}

func (m *PurposeBlock) Reset()                    { *m = PurposeBlock{} }
func (m *PurposeBlock) String() string            { return proto.CompactTextString(m) }
func (*PurposeBlock) ProtoMessage()               {}
func (*PurposeBlock) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{1} }

func (m *PurposeBlock) GetN() uint64 {
	if m != nil {
		return m.N
	}
	return 0
}

func (m *PurposeBlock) GetB() *protos2.Block {
	if m != nil {
		return m.B
	}
	return nil
}

type PurposeTransactions struct {
	Ids []string `protobuf:"bytes,1,rep,name=ids" json:"ids,omitempty"`
}

func (m *PurposeTransactions) Reset()                    { *m = PurposeTransactions{} }
func (m *PurposeTransactions) String() string            { return proto.CompactTextString(m) }
func (*PurposeTransactions) ProtoMessage()               {}
func (*PurposeTransactions) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{2} }

func (m *PurposeTransactions) GetIds() []string {
	if m != nil {
		return m.Ids
	}
	return nil
}

type PurposeBlocks struct {
	Blks []*PurposeBlock `protobuf:"bytes,1,rep,name=blks" json:"blks,omitempty"`
}

func (m *PurposeBlocks) Reset()                    { *m = PurposeBlocks{} }
func (m *PurposeBlocks) String() string            { return proto.CompactTextString(m) }
func (*PurposeBlocks) ProtoMessage()               {}
func (*PurposeBlocks) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{3} }

func (m *PurposeBlocks) GetBlks() []*PurposeBlock {
	if m != nil {
		return m.Blks
	}
	return nil
}

type ConsensusOutput struct {
	// indicate the position the consensus tx corresponding to in
	// the whole chain (history), it may be an estimated value
	// unless out is block or blockhash (in that case, it is precise)
	// if out is error, this value MUST NOT be considered
	Position uint64 `protobuf:"varint,1,opt,name=position" json:"position,omitempty"`
	// Types that are valid to be assigned to Out:
	//	*ConsensusOutput_More
	//	*ConsensusOutput_Block
	//	*ConsensusOutput_Blockhash
	//	*ConsensusOutput_Blocks
	//	*ConsensusOutput_Nothing
	//	*ConsensusOutput_Error
	Out isConsensusOutput_Out `protobuf_oneof:"out"`
}

func (m *ConsensusOutput) Reset()                    { *m = ConsensusOutput{} }
func (m *ConsensusOutput) String() string            { return proto.CompactTextString(m) }
func (*ConsensusOutput) ProtoMessage()               {}
func (*ConsensusOutput) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{4} }

type isConsensusOutput_Out interface {
	isConsensusOutput_Out()
}

type ConsensusOutput_More struct {
	More *PurposeTransactions `protobuf:"bytes,2,opt,name=more,oneof"`
}
type ConsensusOutput_Block struct {
	Block *protos2.Block `protobuf:"bytes,3,opt,name=block,oneof"`
}
type ConsensusOutput_Blockhash struct {
	Blockhash []byte `protobuf:"bytes,4,opt,name=blockhash,proto3,oneof"`
}
type ConsensusOutput_Blocks struct {
	Blocks *PurposeBlocks `protobuf:"bytes,5,opt,name=blocks,oneof"`
}
type ConsensusOutput_Nothing struct {
	Nothing *google_protobuf1.Empty `protobuf:"bytes,8,opt,name=nothing,oneof"`
}
type ConsensusOutput_Error struct {
	Error string `protobuf:"bytes,9,opt,name=error,oneof"`
}

func (*ConsensusOutput_More) isConsensusOutput_Out()      {}
func (*ConsensusOutput_Block) isConsensusOutput_Out()     {}
func (*ConsensusOutput_Blockhash) isConsensusOutput_Out() {}
func (*ConsensusOutput_Blocks) isConsensusOutput_Out()    {}
func (*ConsensusOutput_Nothing) isConsensusOutput_Out()   {}
func (*ConsensusOutput_Error) isConsensusOutput_Out()     {}

func (m *ConsensusOutput) GetOut() isConsensusOutput_Out {
	if m != nil {
		return m.Out
	}
	return nil
}

func (m *ConsensusOutput) GetPosition() uint64 {
	if m != nil {
		return m.Position
	}
	return 0
}

func (m *ConsensusOutput) GetMore() *PurposeTransactions {
	if x, ok := m.GetOut().(*ConsensusOutput_More); ok {
		return x.More
	}
	return nil
}

func (m *ConsensusOutput) GetBlock() *protos2.Block {
	if x, ok := m.GetOut().(*ConsensusOutput_Block); ok {
		return x.Block
	}
	return nil
}

func (m *ConsensusOutput) GetBlockhash() []byte {
	if x, ok := m.GetOut().(*ConsensusOutput_Blockhash); ok {
		return x.Blockhash
	}
	return nil
}

func (m *ConsensusOutput) GetBlocks() *PurposeBlocks {
	if x, ok := m.GetOut().(*ConsensusOutput_Blocks); ok {
		return x.Blocks
	}
	return nil
}

func (m *ConsensusOutput) GetNothing() *google_protobuf1.Empty {
	if x, ok := m.GetOut().(*ConsensusOutput_Nothing); ok {
		return x.Nothing
	}
	return nil
}

func (m *ConsensusOutput) GetError() string {
	if x, ok := m.GetOut().(*ConsensusOutput_Error); ok {
		return x.Error
	}
	return ""
}

// XXX_OneofFuncs is for the internal use of the proto package.
func (*ConsensusOutput) XXX_OneofFuncs() (func(msg proto.Message, b *proto.Buffer) error, func(msg proto.Message, tag, wire int, b *proto.Buffer) (bool, error), func(msg proto.Message) (n int), []interface{}) {
	return _ConsensusOutput_OneofMarshaler, _ConsensusOutput_OneofUnmarshaler, _ConsensusOutput_OneofSizer, []interface{}{
		(*ConsensusOutput_More)(nil),
		(*ConsensusOutput_Block)(nil),
		(*ConsensusOutput_Blockhash)(nil),
		(*ConsensusOutput_Blocks)(nil),
		(*ConsensusOutput_Nothing)(nil),
		(*ConsensusOutput_Error)(nil),
	}
}

func _ConsensusOutput_OneofMarshaler(msg proto.Message, b *proto.Buffer) error {
	m := msg.(*ConsensusOutput)
	// out
	switch x := m.Out.(type) {
	case *ConsensusOutput_More:
		b.EncodeVarint(2<<3 | proto.WireBytes)
		if err := b.EncodeMessage(x.More); err != nil {
			return err
		}
	case *ConsensusOutput_Block:
		b.EncodeVarint(3<<3 | proto.WireBytes)
		if err := b.EncodeMessage(x.Block); err != nil {
			return err
		}
	case *ConsensusOutput_Blockhash:
		b.EncodeVarint(4<<3 | proto.WireBytes)
		b.EncodeRawBytes(x.Blockhash)
	case *ConsensusOutput_Blocks:
		b.EncodeVarint(5<<3 | proto.WireBytes)
		if err := b.EncodeMessage(x.Blocks); err != nil {
			return err
		}
	case *ConsensusOutput_Nothing:
		b.EncodeVarint(8<<3 | proto.WireBytes)
		if err := b.EncodeMessage(x.Nothing); err != nil {
			return err
		}
	case *ConsensusOutput_Error:
		b.EncodeVarint(9<<3 | proto.WireBytes)
		b.EncodeStringBytes(x.Error)
	case nil:
	default:
		return fmt.Errorf("ConsensusOutput.Out has unexpected type %T", x)
	}
	return nil
}

func _ConsensusOutput_OneofUnmarshaler(msg proto.Message, tag, wire int, b *proto.Buffer) (bool, error) {
	m := msg.(*ConsensusOutput)
	switch tag {
	case 2: // out.more
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		msg := new(PurposeTransactions)
		err := b.DecodeMessage(msg)
		m.Out = &ConsensusOutput_More{msg}
		return true, err
	case 3: // out.block
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		msg := new(protos2.Block)
		err := b.DecodeMessage(msg)
		m.Out = &ConsensusOutput_Block{msg}
		return true, err
	case 4: // out.blockhash
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		x, err := b.DecodeRawBytes(true)
		m.Out = &ConsensusOutput_Blockhash{x}
		return true, err
	case 5: // out.blocks
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		msg := new(PurposeBlocks)
		err := b.DecodeMessage(msg)
		m.Out = &ConsensusOutput_Blocks{msg}
		return true, err
	case 8: // out.nothing
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		msg := new(google_protobuf1.Empty)
		err := b.DecodeMessage(msg)
		m.Out = &ConsensusOutput_Nothing{msg}
		return true, err
	case 9: // out.error
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		x, err := b.DecodeStringBytes()
		m.Out = &ConsensusOutput_Error{x}
		return true, err
	default:
		return false, nil
	}
}

func _ConsensusOutput_OneofSizer(msg proto.Message) (n int) {
	m := msg.(*ConsensusOutput)
	// out
	switch x := m.Out.(type) {
	case *ConsensusOutput_More:
		s := proto.Size(x.More)
		n += proto.SizeVarint(2<<3 | proto.WireBytes)
		n += proto.SizeVarint(uint64(s))
		n += s
	case *ConsensusOutput_Block:
		s := proto.Size(x.Block)
		n += proto.SizeVarint(3<<3 | proto.WireBytes)
		n += proto.SizeVarint(uint64(s))
		n += s
	case *ConsensusOutput_Blockhash:
		n += proto.SizeVarint(4<<3 | proto.WireBytes)
		n += proto.SizeVarint(uint64(len(x.Blockhash)))
		n += len(x.Blockhash)
	case *ConsensusOutput_Blocks:
		s := proto.Size(x.Blocks)
		n += proto.SizeVarint(5<<3 | proto.WireBytes)
		n += proto.SizeVarint(uint64(s))
		n += s
	case *ConsensusOutput_Nothing:
		s := proto.Size(x.Nothing)
		n += proto.SizeVarint(8<<3 | proto.WireBytes)
		n += proto.SizeVarint(uint64(s))
		n += s
	case *ConsensusOutput_Error:
		n += proto.SizeVarint(9<<3 | proto.WireBytes)
		n += proto.SizeVarint(uint64(len(x.Error)))
		n += len(x.Error)
	case nil:
	default:
		panic(fmt.Sprintf("proto: unexpected type %T in oneof", x))
	}
	return n
}

func init() {
	proto.RegisterType((*ConsensusPurpose)(nil), "csprotos.ConsensusPurpose")
	proto.RegisterType((*PurposeBlock)(nil), "csprotos.PurposeBlock")
	proto.RegisterType((*PurposeTransactions)(nil), "csprotos.PurposeTransactions")
	proto.RegisterType((*PurposeBlocks)(nil), "csprotos.PurposeBlocks")
	proto.RegisterType((*ConsensusOutput)(nil), "csprotos.ConsensusOutput")
}

func init() { proto.RegisterFile("consensus.proto", fileDescriptor0) }

var fileDescriptor0 = []byte{
	// 370 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x94, 0x91, 0x51, 0x4b, 0xc3, 0x30,
	0x10, 0xc7, 0x9b, 0xb5, 0x9d, 0xeb, 0xd9, 0xb1, 0x11, 0x61, 0x96, 0x0d, 0x65, 0x14, 0xc4, 0x22,
	0xd2, 0xe1, 0xf6, 0x24, 0xbe, 0x4d, 0x84, 0xbe, 0x29, 0xc1, 0x2f, 0xd0, 0xd6, 0x6e, 0x2b, 0xdb,
	0x92, 0x92, 0xa4, 0xa0, 0x9f, 0xc3, 0x0f, 0xe2, 0x57, 0x94, 0xa4, 0xed, 0x2c, 0xba, 0x17, 0xdf,
	0xee, 0x92, 0xff, 0xdd, 0xef, 0x7f, 0x77, 0x30, 0x48, 0x19, 0x15, 0x19, 0x15, 0xa5, 0x08, 0x0b,
	0xce, 0x24, 0xc3, 0xbd, 0x54, 0xe8, 0x40, 0x8c, 0xdd, 0x55, 0x9c, 0xf0, 0x3c, 0xad, 0xde, 0xc7,
	0x93, 0x35, 0x63, 0xeb, 0x5d, 0x36, 0xd3, 0x59, 0x52, 0xae, 0x66, 0xd9, 0xbe, 0x90, 0x1f, 0xd5,
	0xa7, 0xff, 0x89, 0x60, 0xf8, 0xd8, 0x34, 0x7a, 0x29, 0x79, 0xc1, 0x44, 0x86, 0x6f, 0xc1, 0x94,
	0xef, 0xc2, 0x43, 0x53, 0x14, 0x9c, 0xce, 0xbd, 0x4a, 0x29, 0xc2, 0x57, 0x1e, 0x53, 0x11, 0xa7,
	0x32, 0x67, 0x74, 0xb9, 0x63, 0xe9, 0x36, 0x32, 0x88, 0x92, 0xe1, 0x39, 0x9c, 0x50, 0x26, 0x37,
	0x39, 0x5d, 0x7b, 0x1d, 0x5d, 0x31, 0x0a, 0x2b, 0x62, 0xd8, 0x10, 0xc3, 0x27, 0x45, 0x8c, 0x0c,
	0xd2, 0x08, 0xf1, 0x08, 0xec, 0x8c, 0x73, 0xc6, 0x3d, 0x73, 0x8a, 0x02, 0x27, 0x32, 0x48, 0x95,
	0x2e, 0x6d, 0x30, 0x59, 0x29, 0xfd, 0x7b, 0x70, 0x6b, 0x2f, 0x9a, 0x84, 0x5d, 0x40, 0x54, 0xdb,
	0xb1, 0x08, 0xa2, 0x78, 0x02, 0x28, 0xa9, 0x51, 0xfd, 0xc6, 0x9c, 0xd6, 0x11, 0x94, 0xf8, 0xd7,
	0x70, 0x56, 0x97, 0xb6, 0xfc, 0x0a, 0x3c, 0x04, 0x33, 0x7f, 0x53, 0x23, 0x99, 0x81, 0x43, 0x54,
	0xe8, 0x3f, 0x40, 0xbf, 0xcd, 0x10, 0xf8, 0x06, 0xac, 0x64, 0xb7, 0xad, 0x34, 0x6a, 0x88, 0x66,
	0x9d, 0x61, 0x5b, 0x46, 0xb4, 0xc6, 0xff, 0xea, 0xc0, 0xe0, 0xb0, 0xb6, 0xe7, 0x52, 0x16, 0xa5,
	0xc4, 0x63, 0xe8, 0x15, 0x4c, 0xe4, 0x8a, 0x57, 0x7b, 0x3d, 0xe4, 0x78, 0x01, 0xd6, 0x9e, 0xf1,
	0xac, 0x76, 0x7d, 0xf1, 0xa7, 0x77, 0xdb, 0x6b, 0x64, 0x10, 0x2d, 0xc6, 0x57, 0x60, 0x27, 0x8a,
	0xa9, 0x97, 0xf4, 0x7b, 0x56, 0xb5, 0x33, 0xfd, 0x8b, 0x2f, 0xc1, 0xd1, 0xc1, 0x26, 0x16, 0x1b,
	0xcf, 0x9a, 0xa2, 0xc0, 0x8d, 0x0c, 0xf2, 0xf3, 0x84, 0xef, 0xa0, 0xab, 0x13, 0xe1, 0xd9, 0xba,
	0xcf, 0xf9, 0xf1, 0xc9, 0x14, 0xb7, 0x16, 0xb6, 0x4f, 0xda, 0xfb, 0xf7, 0x49, 0x9d, 0x63, 0x27,
	0x4d, 0xba, 0xba, 0x72, 0xf1, 0x1d, 0x00, 0x00, 0xff, 0xff, 0x39, 0x62, 0x13, 0xe5, 0xb7, 0x02,
	0x00, 0x00,
}
