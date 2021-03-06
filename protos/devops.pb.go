// Code generated by protoc-gen-go. DO NOT EDIT.
// source: devops.proto

package protos

import (
	fmt "fmt"
	proto "github.com/golang/protobuf/proto"
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
	math "math"
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

type BuildResult_StatusCode int32

const (
	BuildResult_UNDEFINED BuildResult_StatusCode = 0
	BuildResult_SUCCESS   BuildResult_StatusCode = 1
	BuildResult_FAILURE   BuildResult_StatusCode = 2
)

var BuildResult_StatusCode_name = map[int32]string{
	0: "UNDEFINED",
	1: "SUCCESS",
	2: "FAILURE",
}

var BuildResult_StatusCode_value = map[string]int32{
	"UNDEFINED": 0,
	"SUCCESS":   1,
	"FAILURE":   2,
}

func (x BuildResult_StatusCode) String() string {
	return proto.EnumName(BuildResult_StatusCode_name, int32(x))
}

func (BuildResult_StatusCode) EnumDescriptor() ([]byte, []int) {
	return fileDescriptor_182b548def8538ea, []int{4, 0}
}

// Secret is a temporary object to establish security with the Devops.
// A better solution using certificate will be introduced later
type Secret struct {
	EnrollId             string   `protobuf:"bytes,1,opt,name=enrollId,proto3" json:"enrollId,omitempty"`
	EnrollSecret         string   `protobuf:"bytes,2,opt,name=enrollSecret,proto3" json:"enrollSecret,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *Secret) Reset()         { *m = Secret{} }
func (m *Secret) String() string { return proto.CompactTextString(m) }
func (*Secret) ProtoMessage()    {}
func (*Secret) Descriptor() ([]byte, []int) {
	return fileDescriptor_182b548def8538ea, []int{0}
}

func (m *Secret) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_Secret.Unmarshal(m, b)
}
func (m *Secret) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_Secret.Marshal(b, m, deterministic)
}
func (m *Secret) XXX_Merge(src proto.Message) {
	xxx_messageInfo_Secret.Merge(m, src)
}
func (m *Secret) XXX_Size() int {
	return xxx_messageInfo_Secret.Size(m)
}
func (m *Secret) XXX_DiscardUnknown() {
	xxx_messageInfo_Secret.DiscardUnknown(m)
}

var xxx_messageInfo_Secret proto.InternalMessageInfo

func (m *Secret) GetEnrollId() string {
	if m != nil {
		return m.EnrollId
	}
	return ""
}

func (m *Secret) GetEnrollSecret() string {
	if m != nil {
		return m.EnrollSecret
	}
	return ""
}

type SigmaInput struct {
	Secret               *Secret  `protobuf:"bytes,1,opt,name=secret,proto3" json:"secret,omitempty"`
	AppTCert             []byte   `protobuf:"bytes,2,opt,name=appTCert,proto3" json:"appTCert,omitempty"`
	Data                 []byte   `protobuf:"bytes,3,opt,name=data,proto3" json:"data,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *SigmaInput) Reset()         { *m = SigmaInput{} }
func (m *SigmaInput) String() string { return proto.CompactTextString(m) }
func (*SigmaInput) ProtoMessage()    {}
func (*SigmaInput) Descriptor() ([]byte, []int) {
	return fileDescriptor_182b548def8538ea, []int{1}
}

func (m *SigmaInput) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_SigmaInput.Unmarshal(m, b)
}
func (m *SigmaInput) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_SigmaInput.Marshal(b, m, deterministic)
}
func (m *SigmaInput) XXX_Merge(src proto.Message) {
	xxx_messageInfo_SigmaInput.Merge(m, src)
}
func (m *SigmaInput) XXX_Size() int {
	return xxx_messageInfo_SigmaInput.Size(m)
}
func (m *SigmaInput) XXX_DiscardUnknown() {
	xxx_messageInfo_SigmaInput.DiscardUnknown(m)
}

var xxx_messageInfo_SigmaInput proto.InternalMessageInfo

func (m *SigmaInput) GetSecret() *Secret {
	if m != nil {
		return m.Secret
	}
	return nil
}

func (m *SigmaInput) GetAppTCert() []byte {
	if m != nil {
		return m.AppTCert
	}
	return nil
}

func (m *SigmaInput) GetData() []byte {
	if m != nil {
		return m.Data
	}
	return nil
}

type ExecuteWithBinding struct {
	ChaincodeInvocationSpec *ChaincodeInvocationSpec `protobuf:"bytes,1,opt,name=chaincodeInvocationSpec,proto3" json:"chaincodeInvocationSpec,omitempty"`
	Binding                 []byte                   `protobuf:"bytes,2,opt,name=binding,proto3" json:"binding,omitempty"`
	XXX_NoUnkeyedLiteral    struct{}                 `json:"-"`
	XXX_unrecognized        []byte                   `json:"-"`
	XXX_sizecache           int32                    `json:"-"`
}

func (m *ExecuteWithBinding) Reset()         { *m = ExecuteWithBinding{} }
func (m *ExecuteWithBinding) String() string { return proto.CompactTextString(m) }
func (*ExecuteWithBinding) ProtoMessage()    {}
func (*ExecuteWithBinding) Descriptor() ([]byte, []int) {
	return fileDescriptor_182b548def8538ea, []int{2}
}

func (m *ExecuteWithBinding) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_ExecuteWithBinding.Unmarshal(m, b)
}
func (m *ExecuteWithBinding) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_ExecuteWithBinding.Marshal(b, m, deterministic)
}
func (m *ExecuteWithBinding) XXX_Merge(src proto.Message) {
	xxx_messageInfo_ExecuteWithBinding.Merge(m, src)
}
func (m *ExecuteWithBinding) XXX_Size() int {
	return xxx_messageInfo_ExecuteWithBinding.Size(m)
}
func (m *ExecuteWithBinding) XXX_DiscardUnknown() {
	xxx_messageInfo_ExecuteWithBinding.DiscardUnknown(m)
}

var xxx_messageInfo_ExecuteWithBinding proto.InternalMessageInfo

func (m *ExecuteWithBinding) GetChaincodeInvocationSpec() *ChaincodeInvocationSpec {
	if m != nil {
		return m.ChaincodeInvocationSpec
	}
	return nil
}

func (m *ExecuteWithBinding) GetBinding() []byte {
	if m != nil {
		return m.Binding
	}
	return nil
}

type SigmaOutput struct {
	Tcert                []byte   `protobuf:"bytes,1,opt,name=tcert,proto3" json:"tcert,omitempty"`
	Sigma                []byte   `protobuf:"bytes,2,opt,name=sigma,proto3" json:"sigma,omitempty"`
	Asn1Encoding         []byte   `protobuf:"bytes,3,opt,name=asn1Encoding,proto3" json:"asn1Encoding,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *SigmaOutput) Reset()         { *m = SigmaOutput{} }
func (m *SigmaOutput) String() string { return proto.CompactTextString(m) }
func (*SigmaOutput) ProtoMessage()    {}
func (*SigmaOutput) Descriptor() ([]byte, []int) {
	return fileDescriptor_182b548def8538ea, []int{3}
}

func (m *SigmaOutput) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_SigmaOutput.Unmarshal(m, b)
}
func (m *SigmaOutput) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_SigmaOutput.Marshal(b, m, deterministic)
}
func (m *SigmaOutput) XXX_Merge(src proto.Message) {
	xxx_messageInfo_SigmaOutput.Merge(m, src)
}
func (m *SigmaOutput) XXX_Size() int {
	return xxx_messageInfo_SigmaOutput.Size(m)
}
func (m *SigmaOutput) XXX_DiscardUnknown() {
	xxx_messageInfo_SigmaOutput.DiscardUnknown(m)
}

var xxx_messageInfo_SigmaOutput proto.InternalMessageInfo

func (m *SigmaOutput) GetTcert() []byte {
	if m != nil {
		return m.Tcert
	}
	return nil
}

func (m *SigmaOutput) GetSigma() []byte {
	if m != nil {
		return m.Sigma
	}
	return nil
}

func (m *SigmaOutput) GetAsn1Encoding() []byte {
	if m != nil {
		return m.Asn1Encoding
	}
	return nil
}

type BuildResult struct {
	Status               BuildResult_StatusCode   `protobuf:"varint,1,opt,name=status,proto3,enum=protos.BuildResult_StatusCode" json:"status,omitempty"`
	Msg                  string                   `protobuf:"bytes,2,opt,name=msg,proto3" json:"msg,omitempty"`
	DeploymentSpec       *ChaincodeDeploymentSpec `protobuf:"bytes,3,opt,name=deploymentSpec,proto3" json:"deploymentSpec,omitempty"`
	XXX_NoUnkeyedLiteral struct{}                 `json:"-"`
	XXX_unrecognized     []byte                   `json:"-"`
	XXX_sizecache        int32                    `json:"-"`
}

func (m *BuildResult) Reset()         { *m = BuildResult{} }
func (m *BuildResult) String() string { return proto.CompactTextString(m) }
func (*BuildResult) ProtoMessage()    {}
func (*BuildResult) Descriptor() ([]byte, []int) {
	return fileDescriptor_182b548def8538ea, []int{4}
}

func (m *BuildResult) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_BuildResult.Unmarshal(m, b)
}
func (m *BuildResult) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_BuildResult.Marshal(b, m, deterministic)
}
func (m *BuildResult) XXX_Merge(src proto.Message) {
	xxx_messageInfo_BuildResult.Merge(m, src)
}
func (m *BuildResult) XXX_Size() int {
	return xxx_messageInfo_BuildResult.Size(m)
}
func (m *BuildResult) XXX_DiscardUnknown() {
	xxx_messageInfo_BuildResult.DiscardUnknown(m)
}

var xxx_messageInfo_BuildResult proto.InternalMessageInfo

func (m *BuildResult) GetStatus() BuildResult_StatusCode {
	if m != nil {
		return m.Status
	}
	return BuildResult_UNDEFINED
}

func (m *BuildResult) GetMsg() string {
	if m != nil {
		return m.Msg
	}
	return ""
}

func (m *BuildResult) GetDeploymentSpec() *ChaincodeDeploymentSpec {
	if m != nil {
		return m.DeploymentSpec
	}
	return nil
}

type TransactionRequest struct {
	TransactionUuid      string   `protobuf:"bytes,1,opt,name=transactionUuid,proto3" json:"transactionUuid,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *TransactionRequest) Reset()         { *m = TransactionRequest{} }
func (m *TransactionRequest) String() string { return proto.CompactTextString(m) }
func (*TransactionRequest) ProtoMessage()    {}
func (*TransactionRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_182b548def8538ea, []int{5}
}

func (m *TransactionRequest) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_TransactionRequest.Unmarshal(m, b)
}
func (m *TransactionRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_TransactionRequest.Marshal(b, m, deterministic)
}
func (m *TransactionRequest) XXX_Merge(src proto.Message) {
	xxx_messageInfo_TransactionRequest.Merge(m, src)
}
func (m *TransactionRequest) XXX_Size() int {
	return xxx_messageInfo_TransactionRequest.Size(m)
}
func (m *TransactionRequest) XXX_DiscardUnknown() {
	xxx_messageInfo_TransactionRequest.DiscardUnknown(m)
}

var xxx_messageInfo_TransactionRequest proto.InternalMessageInfo

func (m *TransactionRequest) GetTransactionUuid() string {
	if m != nil {
		return m.TransactionUuid
	}
	return ""
}

func init() {
	proto.RegisterEnum("protos.BuildResult_StatusCode", BuildResult_StatusCode_name, BuildResult_StatusCode_value)
	proto.RegisterType((*Secret)(nil), "protos.Secret")
	proto.RegisterType((*SigmaInput)(nil), "protos.SigmaInput")
	proto.RegisterType((*ExecuteWithBinding)(nil), "protos.ExecuteWithBinding")
	proto.RegisterType((*SigmaOutput)(nil), "protos.SigmaOutput")
	proto.RegisterType((*BuildResult)(nil), "protos.BuildResult")
	proto.RegisterType((*TransactionRequest)(nil), "protos.TransactionRequest")
}

func init() { proto.RegisterFile("devops.proto", fileDescriptor_182b548def8538ea) }

var fileDescriptor_182b548def8538ea = []byte{
	// 525 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0xa4, 0x54, 0x51, 0x6f, 0x12, 0x4d,
	0x14, 0x65, 0xe1, 0x63, 0xfb, 0xf5, 0x82, 0x94, 0xdc, 0x68, 0x24, 0x3c, 0x68, 0x33, 0x0f, 0xa6,
	0x89, 0x09, 0x44, 0x8c, 0x3e, 0xa9, 0x49, 0x81, 0xad, 0x92, 0x34, 0x35, 0xee, 0x96, 0x18, 0x4d,
	0x7c, 0x18, 0x76, 0x47, 0x98, 0x08, 0x33, 0xeb, 0xce, 0x4c, 0x63, 0x7f, 0x82, 0x3f, 0xc9, 0x5f,
	0xe2, 0xdf, 0x31, 0x33, 0xb3, 0x50, 0xc1, 0x36, 0x4d, 0xf4, 0x89, 0x39, 0x67, 0xce, 0xbd, 0x87,
	0x73, 0xf7, 0x66, 0xa0, 0x99, 0xb1, 0x0b, 0x99, 0xab, 0x5e, 0x5e, 0x48, 0x2d, 0x31, 0x74, 0x3f,
	0xaa, 0x7b, 0x90, 0x2e, 0x28, 0x17, 0xa9, 0xcc, 0x98, 0xbf, 0xe8, 0x36, 0x3f, 0xd3, 0x59, 0xc1,
	0x53, 0x8f, 0xc8, 0x1b, 0x08, 0x13, 0x96, 0x16, 0x4c, 0x63, 0x17, 0xfe, 0x67, 0xa2, 0x90, 0xcb,
	0xe5, 0x24, 0xeb, 0x04, 0x87, 0xc1, 0xd1, 0x7e, 0xbc, 0xc1, 0x48, 0xa0, 0xe9, 0xcf, 0x5e, 0xdb,
	0xa9, 0xba, 0xfb, 0x2d, 0x8e, 0x64, 0x00, 0x09, 0x9f, 0xaf, 0xe8, 0x44, 0xe4, 0x46, 0xe3, 0x23,
	0x08, 0x95, 0xd7, 0xda, 0x5e, 0x8d, 0x41, 0xcb, 0xfb, 0xa9, 0x9e, 0x57, 0xc7, 0xe5, 0xad, 0x75,
	0xa5, 0x79, 0x7e, 0x3e, 0x62, 0x85, 0xef, 0xda, 0x8c, 0x37, 0x18, 0x11, 0xfe, 0xcb, 0xa8, 0xa6,
	0x9d, 0x9a, 0xe3, 0xdd, 0x99, 0x7c, 0x0f, 0x00, 0xa3, 0x6f, 0x2c, 0x35, 0x9a, 0xbd, 0xe7, 0x7a,
	0x31, 0xe4, 0x22, 0xe3, 0x62, 0x8e, 0x1f, 0xe0, 0xfe, 0x26, 0xe7, 0x44, 0x5c, 0xc8, 0x94, 0x6a,
	0x2e, 0x45, 0x92, 0xb3, 0xb4, 0xf4, 0x7f, 0xb8, 0xf6, 0x1f, 0x5d, 0x2f, 0x8b, 0x6f, 0xaa, 0xc7,
	0x0e, 0xec, 0xcd, 0xbc, 0x4b, 0xf9, 0x07, 0xd7, 0x90, 0x7c, 0x82, 0x86, 0x4b, 0xfc, 0xd6, 0x68,
	0x1b, 0xf9, 0x2e, 0xd4, 0x75, 0x6a, 0x73, 0x04, 0x4e, 0xe6, 0x81, 0x65, 0x95, 0x15, 0x95, 0xc5,
	0x1e, 0xd8, 0x81, 0x52, 0x25, 0x9e, 0x44, 0xd6, 0xd0, 0x76, 0xf6, 0x11, 0xb7, 0x38, 0xf2, 0x33,
	0x80, 0xc6, 0xd0, 0xf0, 0x65, 0x16, 0x33, 0x65, 0x96, 0x1a, 0x9f, 0x43, 0xa8, 0x34, 0xd5, 0x46,
	0x39, 0x83, 0xd6, 0xe0, 0xc1, 0x3a, 0xd2, 0x6f, 0xa2, 0x5e, 0xe2, 0x14, 0x23, 0x99, 0xb1, 0xb8,
	0x54, 0x63, 0x1b, 0x6a, 0x2b, 0x35, 0x2f, 0xbf, 0x99, 0x3d, 0xe2, 0x6b, 0x68, 0x65, 0x2c, 0x5f,
	0xca, 0xcb, 0x15, 0x13, 0xda, 0x0d, 0xa9, 0x76, 0xc3, 0x90, 0xc6, 0x5b, 0xb2, 0x78, 0xa7, 0x8c,
	0x3c, 0x03, 0xb8, 0x32, 0xc4, 0x3b, 0xb0, 0x3f, 0x3d, 0x1b, 0x47, 0x27, 0x93, 0xb3, 0x68, 0xdc,
	0xae, 0x60, 0x03, 0xf6, 0x92, 0xe9, 0x68, 0x14, 0x25, 0x49, 0x3b, 0xb0, 0xe0, 0xe4, 0x78, 0x72,
	0x3a, 0x8d, 0xa3, 0x76, 0x95, 0xbc, 0x02, 0x3c, 0x2f, 0xa8, 0x50, 0x34, 0xb5, 0x53, 0x8e, 0xd9,
	0x57, 0xc3, 0x94, 0xc6, 0x23, 0x38, 0xd0, 0x57, 0xec, 0xd4, 0xf0, 0xf5, 0x1e, 0xee, 0xd2, 0x83,
	0x1f, 0x55, 0x08, 0xc7, 0x6e, 0xd9, 0xf1, 0x31, 0xd4, 0x4f, 0xe5, 0x9c, 0x0b, 0xdc, 0x59, 0xb0,
	0x6e, 0x7b, 0x8d, 0x63, 0xa6, 0x72, 0x29, 0x14, 0x23, 0x15, 0x3c, 0x86, 0xba, 0x9b, 0x15, 0xde,
	0xfb, 0x23, 0xa8, 0x8d, 0xd3, 0xbd, 0x2d, 0x3f, 0xa9, 0xe0, 0xd0, 0x3a, 0x5b, 0xee, 0x1f, 0x7a,
	0xbc, 0x84, 0xd0, 0xee, 0xd8, 0x17, 0x86, 0xb7, 0x6d, 0xe5, 0xb5, 0x29, 0x5e, 0x40, 0xfd, 0x9d,
	0x61, 0xc5, 0xe5, 0x5f, 0x55, 0x0f, 0xc9, 0xc7, 0xc3, 0x39, 0xd7, 0x0b, 0x33, 0xeb, 0xa5, 0x72,
	0xd5, 0xa7, 0x33, 0xb7, 0xf6, 0x7d, 0xff, 0x26, 0xf4, 0xbd, 0x7c, 0xe6, 0xdf, 0x8e, 0xa7, 0xbf,
	0x02, 0x00, 0x00, 0xff, 0xff, 0x25, 0x34, 0x1a, 0xcf, 0x52, 0x04, 0x00, 0x00,
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// DevopsClient is the client API for Devops service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type DevopsClient interface {
	// Log in - passed Secret object and returns Response object, where
	// msg is the security context to be used in subsequent invocations
	Login(ctx context.Context, in *Secret, opts ...grpc.CallOption) (*Response, error)
	// Build the chaincode package.
	Build(ctx context.Context, in *ChaincodeSpec, opts ...grpc.CallOption) (*ChaincodeDeploymentSpec, error)
	// Deploy the chaincode package to the chain.
	Deploy(ctx context.Context, in *ChaincodeSpec, opts ...grpc.CallOption) (*ChaincodeDeploymentSpec, error)
	// Invoke chaincode.
	Invoke(ctx context.Context, in *ChaincodeInvocationSpec, opts ...grpc.CallOption) (*Response, error)
	// Query chaincode.
	Query(ctx context.Context, in *ChaincodeInvocationSpec, opts ...grpc.CallOption) (*Response, error)
}

type devopsClient struct {
	cc *grpc.ClientConn
}

func NewDevopsClient(cc *grpc.ClientConn) DevopsClient {
	return &devopsClient{cc}
}

func (c *devopsClient) Login(ctx context.Context, in *Secret, opts ...grpc.CallOption) (*Response, error) {
	out := new(Response)
	err := c.cc.Invoke(ctx, "/protos.Devops/Login", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *devopsClient) Build(ctx context.Context, in *ChaincodeSpec, opts ...grpc.CallOption) (*ChaincodeDeploymentSpec, error) {
	out := new(ChaincodeDeploymentSpec)
	err := c.cc.Invoke(ctx, "/protos.Devops/Build", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *devopsClient) Deploy(ctx context.Context, in *ChaincodeSpec, opts ...grpc.CallOption) (*ChaincodeDeploymentSpec, error) {
	out := new(ChaincodeDeploymentSpec)
	err := c.cc.Invoke(ctx, "/protos.Devops/Deploy", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *devopsClient) Invoke(ctx context.Context, in *ChaincodeInvocationSpec, opts ...grpc.CallOption) (*Response, error) {
	out := new(Response)
	err := c.cc.Invoke(ctx, "/protos.Devops/Invoke", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *devopsClient) Query(ctx context.Context, in *ChaincodeInvocationSpec, opts ...grpc.CallOption) (*Response, error) {
	out := new(Response)
	err := c.cc.Invoke(ctx, "/protos.Devops/Query", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DevopsServer is the server API for Devops service.
type DevopsServer interface {
	// Log in - passed Secret object and returns Response object, where
	// msg is the security context to be used in subsequent invocations
	Login(context.Context, *Secret) (*Response, error)
	// Build the chaincode package.
	Build(context.Context, *ChaincodeSpec) (*ChaincodeDeploymentSpec, error)
	// Deploy the chaincode package to the chain.
	Deploy(context.Context, *ChaincodeSpec) (*ChaincodeDeploymentSpec, error)
	// Invoke chaincode.
	Invoke(context.Context, *ChaincodeInvocationSpec) (*Response, error)
	// Query chaincode.
	Query(context.Context, *ChaincodeInvocationSpec) (*Response, error)
}

func RegisterDevopsServer(s *grpc.Server, srv DevopsServer) {
	s.RegisterService(&_Devops_serviceDesc, srv)
}

func _Devops_Login_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(Secret)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DevopsServer).Login(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/protos.Devops/Login",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DevopsServer).Login(ctx, req.(*Secret))
	}
	return interceptor(ctx, in, info, handler)
}

func _Devops_Build_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ChaincodeSpec)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DevopsServer).Build(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/protos.Devops/Build",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DevopsServer).Build(ctx, req.(*ChaincodeSpec))
	}
	return interceptor(ctx, in, info, handler)
}

func _Devops_Deploy_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ChaincodeSpec)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DevopsServer).Deploy(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/protos.Devops/Deploy",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DevopsServer).Deploy(ctx, req.(*ChaincodeSpec))
	}
	return interceptor(ctx, in, info, handler)
}

func _Devops_Invoke_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ChaincodeInvocationSpec)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DevopsServer).Invoke(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/protos.Devops/Invoke",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DevopsServer).Invoke(ctx, req.(*ChaincodeInvocationSpec))
	}
	return interceptor(ctx, in, info, handler)
}

func _Devops_Query_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ChaincodeInvocationSpec)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DevopsServer).Query(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/protos.Devops/Query",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DevopsServer).Query(ctx, req.(*ChaincodeInvocationSpec))
	}
	return interceptor(ctx, in, info, handler)
}

var _Devops_serviceDesc = grpc.ServiceDesc{
	ServiceName: "protos.Devops",
	HandlerType: (*DevopsServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Login",
			Handler:    _Devops_Login_Handler,
		},
		{
			MethodName: "Build",
			Handler:    _Devops_Build_Handler,
		},
		{
			MethodName: "Deploy",
			Handler:    _Devops_Deploy_Handler,
		},
		{
			MethodName: "Invoke",
			Handler:    _Devops_Invoke_Handler,
		},
		{
			MethodName: "Query",
			Handler:    _Devops_Query_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "devops.proto",
}
