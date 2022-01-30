package sdm

import (
	"context"
	"time"
)

type Service interface {
	GetDevices(ctx context.Context, projectID string) ([]*Device, error)
	GenerateRTSPStream(ctx context.Context, deviceID string) (*CommandResponseGenerateRTSPStream, error)
	ExtendToken(ctx context.Context, device *Device, token string) (*CommandResponseExtendRtspStream, error)
	GetDeviceMeta(id string) (*Meta, error)
	UpdateDeviceMeta(meta *Meta) error
	Subscribe(f func(*Event)) func()
}

type DeviceInfo struct {
	CustomName string `json:"customName"`
}

type CameraResolution struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type CameraLiveStream struct {
	MaxVideoResolution CameraResolution `json:"maxImageResolution"`
	VideoCodecs        []string         `json:"videoCodecs"`
	AudioCodecs        []string         `json:"audioCodecs"`
	SupportedProtocols []string         `json:"supportedProtocols"`
}

type CameraImage struct {
	Max CameraResolution `json:"maxImageResolution"`
}

type CameraPerson struct{}
type CameraSound struct{}
type CameraMotion struct{}
type CameraEventImage struct{}
type CameraDoorbellChime struct{}

type DeviceTraits struct {
	Info          *DeviceInfo          `json:"sdm.devices.traits.Info"`
	LiveStream    *CameraLiveStream    `json:"sdm.devices.traits.CameraLiveStream"`
	Image         *CameraImage         `json:"sdm.devices.traits.CameraImage"`
	Person        *CameraPerson        `json:"sdm.devices.traits.CameraPerson"`
	Sound         *CameraSound         `json:"sdm.devices.traits.CameraSound"`
	Motion        *CameraMotion        `json:"sdm.devices.traits.CameraMotion"`
	EventImage    *CameraEventImage    `json:"sdm.devices.traits.CameraEventImage"`
	DoorbellChime *CameraDoorbellChime `json:"sdm.devices.traits.CameraDoorbellChime"`
}

type Relations struct {
	Parent      string `json:"parent"`
	DisplayName string `json:"displayName"`
}

type Device struct {
	Name            string       `json:"name"`
	Type            string       `json:"type"`
	Assignee        string       `json:"assignee"`
	Traits          DeviceTraits `json:"traits"`
	ParentRelations []Relations  `json:"parentRelations"`
}

type CommandRequest struct {
	Command string            `json:"command"`
	Params  map[string]string `json:"params"`
}

type CommandResponseGenerateRTSPStream struct {
	URLS           map[string]string `json:"streamUrls"`
	ExtensionToken string            `json:"streamExtensionToken"`
	Token          string            `json:"streamToken"`
	ExpiresAt      time.Time         `json:"expiresAt"`
}

type CommandResponseExtendRtspStream struct {
	ExtensionToken string    `json:"streamExtensionToken"`
	Token          string    `json:"streamToken"`
	ExpiresAt      time.Time `json:"expiresAt"`
}

type Meta struct {
	Id      string
	Name    string
	Enabled bool
}
