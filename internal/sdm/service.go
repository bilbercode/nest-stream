package sdm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type service struct {
	client *http.Client
}

func NewService(client *http.Client) Service {
	return &service{client: client}
}

func (s *service) GetDevices(ctx context.Context, projectID string) ([]*Device, error) {
	res, err := s.client.Get(fmt.Sprintf("https://smartdevicemanagement.googleapis.com/v1/enterprises/%s/devices/", projectID))
	if err != nil {
		return nil, fmt.Errorf("failed to query devices from google: %w", err)
	}
	var devices struct {
		Devices []*Device `json:"devices"`
	}
	err = json.NewDecoder(res.Body).Decode(&devices)
	_ = res.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to decode response from google SDM: %w", err)
	}

	return devices.Devices, nil
}

func (s *service) GenerateRTSPStream(ctx context.Context, device *Device) (*CommandResponseGenerateRTSPStream, error) {
	body := bytes.NewBuffer(nil)
	_ = json.NewEncoder(body).Encode(&CommandRequest{
		Command: "sdm.devices.commands.CameraLiveStream.GenerateRtspStream",
	})
	res, err := s.client.Post(fmt.Sprintf("https://smartdevicemanagement.googleapis.com/v1/%s:executeCommand", device.Name), "application/json", body)
	if err != nil {
		return nil, fmt.Errorf("failed to request RTSP stream for device from google: %w", err)
	}

	var details struct {
		Results *CommandResponseGenerateRTSPStream `json:"results"`
	}
	err = json.NewDecoder(res.Body).Decode(&details)
	_ = res.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to decode response from google SDM: %w", err)
	}

	return details.Results, nil
}

func (s *service) ExtendToken(ctx context.Context, device *Device, token string) (*CommandResponseExtendRtspStream, error) {
	body := bytes.NewBuffer(nil)
	_ = json.NewEncoder(body).Encode(&CommandRequest{
		Command: "sdm.devices.commands.CameraLiveStream.ExtendRtspStream",
		Params: map[string]string{
			"streamExtensionToken": token,
		},
	})
	res, err := s.client.Post(fmt.Sprintf("https://smartdevicemanagement.googleapis.com/v1/%s:executeCommand", device.Name), "application/json", body)
	if err != nil {
		panic(err.Error())
	}
	var details struct {
		Results *CommandResponseExtendRtspStream `json:"results"`
	}
	err = json.NewDecoder(res.Body).Decode(&details)
	_ = res.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to decode response from google SDM: %w", err)
	}

	return details.Results, nil
}
