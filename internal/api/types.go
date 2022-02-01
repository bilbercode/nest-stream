package api

import (
	"github.com/bilbercode/nest-stream/internal/devices"
	"github.com/bilbercode/nest-stream/pkg/nest_stream"
)

type NestStreamAPI interface {
	nest_stream.CameraServiceServer
	SetSDMService(service devices.Service, projectID string)
}
