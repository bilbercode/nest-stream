package api

import (
	"github.com/bilbercode/nest-stream/internal/sdm"
	"github.com/bilbercode/nest-stream/pkg/nest_stream"
)

type NestStreamAPI interface {
	nest_stream.CameraServiceServer
	SetSDMService(service sdm.Service, projectID string)
}
