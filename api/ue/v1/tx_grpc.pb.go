// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.3.0
// - protoc             (unknown)
// source: ue/v1/tx.proto

package uev1

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

const (
	Msg_UpdateParams_FullMethodName      = "/ue.v1.Msg/UpdateParams"
	Msg_DeployUEA_FullMethodName         = "/ue.v1.Msg/DeployUEA"
	Msg_MintPC_FullMethodName            = "/ue.v1.Msg/MintPC"
	Msg_ExecutePayload_FullMethodName    = "/ue.v1.Msg/ExecutePayload"
	Msg_AddChainConfig_FullMethodName    = "/ue.v1.Msg/AddChainConfig"
	Msg_UpdateChainConfig_FullMethodName = "/ue.v1.Msg/UpdateChainConfig"
)

// MsgClient is the client API for Msg service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type MsgClient interface {
	// UpdateParams defines a governance operation for updating the parameters.
	//
	// Since: cosmos-sdk 0.47
	UpdateParams(ctx context.Context, in *MsgUpdateParams, opts ...grpc.CallOption) (*MsgUpdateParamsResponse, error)
	// DeployUEA defines a message to deploy a new smart account.
	DeployUEA(ctx context.Context, in *MsgDeployUEA, opts ...grpc.CallOption) (*MsgDeployUEAResponse, error)
	// MintPC defines a message to mint PC tokens to a smart account,
	MintPC(ctx context.Context, in *MsgMintPC, opts ...grpc.CallOption) (*MsgMintPCResponse, error)
	// ExecutePayload defines a message for executing a universal payload
	ExecutePayload(ctx context.Context, in *MsgExecutePayload, opts ...grpc.CallOption) (*MsgExecutePayloadResponse, error)
	// AddChainConfig adds a new ChainConfig entry
	AddChainConfig(ctx context.Context, in *MsgAddChainConfig, opts ...grpc.CallOption) (*MsgAddChainConfigResponse, error)
	// UpdateChainConfig adds a new ChainConfig entry
	UpdateChainConfig(ctx context.Context, in *MsgUpdateChainConfig, opts ...grpc.CallOption) (*MsgUpdateChainConfigResponse, error)
}

type msgClient struct {
	cc grpc.ClientConnInterface
}

func NewMsgClient(cc grpc.ClientConnInterface) MsgClient {
	return &msgClient{cc}
}

func (c *msgClient) UpdateParams(ctx context.Context, in *MsgUpdateParams, opts ...grpc.CallOption) (*MsgUpdateParamsResponse, error) {
	out := new(MsgUpdateParamsResponse)
	err := c.cc.Invoke(ctx, Msg_UpdateParams_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *msgClient) DeployUEA(ctx context.Context, in *MsgDeployUEA, opts ...grpc.CallOption) (*MsgDeployUEAResponse, error) {
	out := new(MsgDeployUEAResponse)
	err := c.cc.Invoke(ctx, Msg_DeployUEA_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *msgClient) MintPC(ctx context.Context, in *MsgMintPC, opts ...grpc.CallOption) (*MsgMintPCResponse, error) {
	out := new(MsgMintPCResponse)
	err := c.cc.Invoke(ctx, Msg_MintPC_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *msgClient) ExecutePayload(ctx context.Context, in *MsgExecutePayload, opts ...grpc.CallOption) (*MsgExecutePayloadResponse, error) {
	out := new(MsgExecutePayloadResponse)
	err := c.cc.Invoke(ctx, Msg_ExecutePayload_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *msgClient) AddChainConfig(ctx context.Context, in *MsgAddChainConfig, opts ...grpc.CallOption) (*MsgAddChainConfigResponse, error) {
	out := new(MsgAddChainConfigResponse)
	err := c.cc.Invoke(ctx, Msg_AddChainConfig_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *msgClient) UpdateChainConfig(ctx context.Context, in *MsgUpdateChainConfig, opts ...grpc.CallOption) (*MsgUpdateChainConfigResponse, error) {
	out := new(MsgUpdateChainConfigResponse)
	err := c.cc.Invoke(ctx, Msg_UpdateChainConfig_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// MsgServer is the server API for Msg service.
// All implementations must embed UnimplementedMsgServer
// for forward compatibility
type MsgServer interface {
	// UpdateParams defines a governance operation for updating the parameters.
	//
	// Since: cosmos-sdk 0.47
	UpdateParams(context.Context, *MsgUpdateParams) (*MsgUpdateParamsResponse, error)
	// DeployUEA defines a message to deploy a new smart account.
	DeployUEA(context.Context, *MsgDeployUEA) (*MsgDeployUEAResponse, error)
	// MintPC defines a message to mint PC tokens to a smart account,
	MintPC(context.Context, *MsgMintPC) (*MsgMintPCResponse, error)
	// ExecutePayload defines a message for executing a universal payload
	ExecutePayload(context.Context, *MsgExecutePayload) (*MsgExecutePayloadResponse, error)
	// AddChainConfig adds a new ChainConfig entry
	AddChainConfig(context.Context, *MsgAddChainConfig) (*MsgAddChainConfigResponse, error)
	// UpdateChainConfig adds a new ChainConfig entry
	UpdateChainConfig(context.Context, *MsgUpdateChainConfig) (*MsgUpdateChainConfigResponse, error)
	mustEmbedUnimplementedMsgServer()
}

// UnimplementedMsgServer must be embedded to have forward compatible implementations.
type UnimplementedMsgServer struct {
}

func (UnimplementedMsgServer) UpdateParams(context.Context, *MsgUpdateParams) (*MsgUpdateParamsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateParams not implemented")
}
func (UnimplementedMsgServer) DeployUEA(context.Context, *MsgDeployUEA) (*MsgDeployUEAResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeployUEA not implemented")
}
func (UnimplementedMsgServer) MintPC(context.Context, *MsgMintPC) (*MsgMintPCResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method MintPC not implemented")
}
func (UnimplementedMsgServer) ExecutePayload(context.Context, *MsgExecutePayload) (*MsgExecutePayloadResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ExecutePayload not implemented")
}
func (UnimplementedMsgServer) AddChainConfig(context.Context, *MsgAddChainConfig) (*MsgAddChainConfigResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method AddChainConfig not implemented")
}
func (UnimplementedMsgServer) UpdateChainConfig(context.Context, *MsgUpdateChainConfig) (*MsgUpdateChainConfigResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateChainConfig not implemented")
}
func (UnimplementedMsgServer) mustEmbedUnimplementedMsgServer() {}

// UnsafeMsgServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to MsgServer will
// result in compilation errors.
type UnsafeMsgServer interface {
	mustEmbedUnimplementedMsgServer()
}

func RegisterMsgServer(s grpc.ServiceRegistrar, srv MsgServer) {
	s.RegisterService(&Msg_ServiceDesc, srv)
}

func _Msg_UpdateParams_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgUpdateParams)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).UpdateParams(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Msg_UpdateParams_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).UpdateParams(ctx, req.(*MsgUpdateParams))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_DeployUEA_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgDeployUEA)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).DeployUEA(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Msg_DeployUEA_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).DeployUEA(ctx, req.(*MsgDeployUEA))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_MintPC_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgMintPC)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).MintPC(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Msg_MintPC_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).MintPC(ctx, req.(*MsgMintPC))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_ExecutePayload_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgExecutePayload)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).ExecutePayload(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Msg_ExecutePayload_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).ExecutePayload(ctx, req.(*MsgExecutePayload))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_AddChainConfig_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgAddChainConfig)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).AddChainConfig(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Msg_AddChainConfig_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).AddChainConfig(ctx, req.(*MsgAddChainConfig))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_UpdateChainConfig_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgUpdateChainConfig)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).UpdateChainConfig(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Msg_UpdateChainConfig_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).UpdateChainConfig(ctx, req.(*MsgUpdateChainConfig))
	}
	return interceptor(ctx, in, info, handler)
}

// Msg_ServiceDesc is the grpc.ServiceDesc for Msg service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var Msg_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "ue.v1.Msg",
	HandlerType: (*MsgServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "UpdateParams",
			Handler:    _Msg_UpdateParams_Handler,
		},
		{
			MethodName: "DeployUEA",
			Handler:    _Msg_DeployUEA_Handler,
		},
		{
			MethodName: "MintPC",
			Handler:    _Msg_MintPC_Handler,
		},
		{
			MethodName: "ExecutePayload",
			Handler:    _Msg_ExecutePayload_Handler,
		},
		{
			MethodName: "AddChainConfig",
			Handler:    _Msg_AddChainConfig_Handler,
		},
		{
			MethodName: "UpdateChainConfig",
			Handler:    _Msg_UpdateChainConfig_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "ue/v1/tx.proto",
}
