package devices

import (
	"context"

	"google.golang.org/genproto/googleapis/home/enterprise/sdm/v1"
)

type EventType int

const (
	EventTypeUnknown EventType = iota
	EventTypeStartCamera
	EventTypeStopCamera
)

type Service interface {
	GetDevices(ctx context.Context) ([]*sdm.Device, error)
	RequestRTSPURL(ctx context.Context, deviceName string) (string, error)
	GetDeviceMeta(id string) (*Meta, error)
	UpdateDeviceMeta(meta *Meta) error
	Subscribe(f func(*Event)) func()
}

type Meta struct {
	Id      string
	Name    string
	Enabled bool
}

type Event struct {
	Type EventType
	Meta *Meta
}
