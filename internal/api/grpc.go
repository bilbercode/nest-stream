package api

import (
	"context"
	"strings"

	"github.com/bilbercode/nest-stream/internal/sdm"
	"github.com/bilbercode/nest-stream/pkg/nest_stream"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type grpcAPI struct {
	nest_stream.UnimplementedCameraServiceServer
	projectID string
	sdm       sdm.Service
}

func (g *grpcAPI) SetSDMService(service sdm.Service, projectID string) {
	g.projectID = projectID
	g.sdm = service
}

func (g *grpcAPI) ListCameras(ctx context.Context, _ *emptypb.Empty) (*nest_stream.Cameras, error) {
	if g.sdm == nil {
		return nil, status.Error(codes.Unavailable, "no client available, please check you have completed oauth2 process")
	}

	devices, err := g.sdm.GetDevices(ctx, g.projectID)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if devices == nil {
		return nil, status.Errorf(codes.NotFound, "no cameras returned")
	}

	var cameras []*nest_stream.Camera
	for _, device := range devices {
		if device.Traits.LiveStream != nil && supportsRTSPProtocol(device.Traits.LiveStream.SupportedProtocols) {
			meta, _ := g.sdm.GetDeviceMeta(device.Name)
			if meta != nil {
				cameras = append(cameras, &nest_stream.Camera{
					Id:      device.Name,
					Name:    meta.Name,
					Enabled: meta.Enabled,
					Type:    strings.Replace(device.Type, "sdm.devices.types.", "", 1),
				})
				continue
			}
			cameras = append(cameras, &nest_stream.Camera{
				Id:      device.Name,
				Type:    strings.Replace(device.Type, "sdm.devices.types.", "", 1),
				Enabled: false,
			})
		}
	}

	if len(cameras) < 1 {
		return nil, status.Error(codes.NotFound, "no compatible cameras")
	}

	return &nest_stream.Cameras{Devices: cameras}, nil
}

func (g *grpcAPI) UpdateCamera(ctx context.Context, camera *nest_stream.Camera) (*nest_stream.Camera, error) {
	if g.sdm == nil {
		return nil, status.Error(codes.Unavailable, "no client available, please check you have completed oauth2 process")
	}
	err := g.sdm.UpdateDeviceMeta(&sdm.Meta{Id: camera.Id, Name: camera.Name, Enabled: camera.Enabled})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return camera, nil
}

func NewGRPCAPI() NestStreamAPI {
	return &grpcAPI{}
}

func supportsRTSPProtocol(protocols []string) bool {
	for _, protocol := range protocols {
		if protocol == "RTSP" {
			return true
		}
	}
	return false
}
