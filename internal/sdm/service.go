package sdm

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/google/uuid"
)

type EventType int

const (
	EventTypeUnknown EventType = iota
	EventTypeStartCamera
	EventTypeStopCamera
)

type Event struct {
	Type EventType
	Meta *Meta
}

type service struct {
	sync.Mutex

	client *http.Client

	eventSubscribers map[string]func(event *Event)
	active           map[string]*Meta

	databaseFolder string
}

func NewService(client *http.Client, databaseRoot string) (Service, error) {
	err := os.MkdirAll(databaseRoot, 0744)
	if err != nil {
		return nil, fmt.Errorf("failed to create database directory %s: %w", databaseRoot, err)
	}
	return &service{
		databaseFolder:   databaseRoot,
		active:           make(map[string]*Meta),
		eventSubscribers: make(map[string]func(*Event)),
		client:           client,
	}, nil
}

func (s *service) getCamerasMeta() ([]*Meta, error) {
	dir, err := os.ReadDir(s.databaseFolder)
	if err != nil {
		return nil, fmt.Errorf("failed to list database directory content: %w", err)
	}

	var cameras []*Meta

	for _, entry := range dir {
		if entry.IsDir() {
			continue
		}

		if strings.HasSuffix(entry.Name(), ".ns") {
			file, err := os.Open(path.Join(s.databaseFolder, entry.Name()))
			if err != nil {
				return nil, fmt.Errorf("failed to read entry %s; %w", entry.Name(), err)
			}
			var meta Meta
			err = gob.NewDecoder(file).Decode(&meta)
			_ = file.Close()
			if err != nil {
				return nil, fmt.Errorf("failed to unmarshal database entry %s: %w", entry.Name(), err)
			}
			cameras = append(cameras, &meta)
		}
	}

	return cameras, nil
}

func (s *service) Subscribe(f func(*Event)) func() {
	id := uuid.NewString()
	s.Lock()
	defer s.Unlock()
	s.eventSubscribers[id] = f

	cameras, _ := s.getCamerasMeta()
	for _, cam := range cameras {
		if cam.Enabled {
			f(&Event{Type: EventTypeStartCamera, Meta: cam})
		}
	}
	return func() {
		s.Lock()
		defer s.Unlock()
		delete(s.eventSubscribers, id)
	}
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

func (s *service) GenerateRTSPStream(ctx context.Context, deviceID string) (*CommandResponseGenerateRTSPStream, error) {
	body := bytes.NewBuffer(nil)
	_ = json.NewEncoder(body).Encode(&CommandRequest{
		Command: "sdm.devices.commands.CameraLiveStream.GenerateRtspStream",
	})
	res, err := s.client.Post(fmt.Sprintf("https://smartdevicemanagement.googleapis.com/v1/%s:executeCommand", deviceID), "application/json", body)
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

func (s *service) GetDeviceMeta(id string) (*Meta, error) {
	s.Lock()
	defer s.Unlock()

	hash := md5.New()
	hash.Write([]byte(id))
	filename := hex.EncodeToString(hash.Sum(nil)) + ".ns"

	file, err := os.Open(path.Join(s.databaseFolder, filename))
	if err != nil {
		return nil, fmt.Errorf("failed to open device file %s: %w", id, err)
	}

	var meta Meta
	err = gob.NewDecoder(file).Decode(&meta)
	_ = file.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to decode database result: %w", err)
	}
	return &meta, nil
}

func (s *service) UpdateDeviceMeta(meta *Meta) error {
	s.Lock()
	defer s.Unlock()

	hash := md5.New()
	hash.Write([]byte(meta.Id))
	filename := hex.EncodeToString(hash.Sum(nil)) + ".ns"

	file, err := os.OpenFile(path.Join(s.databaseFolder, filename), os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0744)
	if err != nil {
		return fmt.Errorf("failed to open database file for writing: %w", err)
	}
	err = gob.NewEncoder(file).Encode(meta)
	_ = file.Close()
	if err != nil {
		return fmt.Errorf("failed to write device meta to file: %w", err)
	}

	_, active := s.active[meta.Id]

	switch {
	case meta.Enabled && !active:
		s.active[meta.Id] = meta
		for _, h := range s.eventSubscribers {
			h(&Event{Type: EventTypeStartCamera, Meta: meta})
		}
	case !meta.Enabled && active:
		delete(s.active, meta.Id)
		for _, h := range s.eventSubscribers {
			h(&Event{Type: EventTypeStopCamera, Meta: meta})
		}
	}
	return nil
}
