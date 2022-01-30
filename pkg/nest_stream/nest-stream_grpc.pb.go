// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.2.0
// - protoc             v3.19.1
// source: nest-stream.proto

package nest_stream

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

// CameraServiceClient is the client API for CameraService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type CameraServiceClient interface {
	ListCameras(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*Cameras, error)
	UpdateCamera(ctx context.Context, in *Camera, opts ...grpc.CallOption) (*Camera, error)
}

type cameraServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewCameraServiceClient(cc grpc.ClientConnInterface) CameraServiceClient {
	return &cameraServiceClient{cc}
}

func (c *cameraServiceClient) ListCameras(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*Cameras, error) {
	out := new(Cameras)
	err := c.cc.Invoke(ctx, "/nest_stream.CameraService/ListCameras", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *cameraServiceClient) UpdateCamera(ctx context.Context, in *Camera, opts ...grpc.CallOption) (*Camera, error) {
	out := new(Camera)
	err := c.cc.Invoke(ctx, "/nest_stream.CameraService/UpdateCamera", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CameraServiceServer is the server API for CameraService service.
// All implementations must embed UnimplementedCameraServiceServer
// for forward compatibility
type CameraServiceServer interface {
	ListCameras(context.Context, *emptypb.Empty) (*Cameras, error)
	UpdateCamera(context.Context, *Camera) (*Camera, error)
	mustEmbedUnimplementedCameraServiceServer()
}

// UnimplementedCameraServiceServer must be embedded to have forward compatible implementations.
type UnimplementedCameraServiceServer struct {
}

func (UnimplementedCameraServiceServer) ListCameras(context.Context, *emptypb.Empty) (*Cameras, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListCameras not implemented")
}
func (UnimplementedCameraServiceServer) UpdateCamera(context.Context, *Camera) (*Camera, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateCamera not implemented")
}
func (UnimplementedCameraServiceServer) mustEmbedUnimplementedCameraServiceServer() {}

// UnsafeCameraServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to CameraServiceServer will
// result in compilation errors.
type UnsafeCameraServiceServer interface {
	mustEmbedUnimplementedCameraServiceServer()
}

func RegisterCameraServiceServer(s grpc.ServiceRegistrar, srv CameraServiceServer) {
	s.RegisterService(&CameraService_ServiceDesc, srv)
}

func _CameraService_ListCameras_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(emptypb.Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CameraServiceServer).ListCameras(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/nest_stream.CameraService/ListCameras",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CameraServiceServer).ListCameras(ctx, req.(*emptypb.Empty))
	}
	return interceptor(ctx, in, info, handler)
}

func _CameraService_UpdateCamera_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(Camera)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CameraServiceServer).UpdateCamera(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/nest_stream.CameraService/UpdateCamera",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CameraServiceServer).UpdateCamera(ctx, req.(*Camera))
	}
	return interceptor(ctx, in, info, handler)
}

// CameraService_ServiceDesc is the grpc.ServiceDesc for CameraService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var CameraService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "nest_stream.CameraService",
	HandlerType: (*CameraServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "ListCameras",
			Handler:    _CameraService_ListCameras_Handler,
		},
		{
			MethodName: "UpdateCamera",
			Handler:    _CameraService_UpdateCamera_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "nest-stream.proto",
}
