package devices

import (
	"context"
	"crypto/md5"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/google/uuid"

	"google.golang.org/genproto/googleapis/home/enterprise/sdm/v1"

	"golang.org/x/oauth2"
	"google.golang.org/api/option"
	gtransport "google.golang.org/api/transport/grpc"
)

type service struct {
	sync.Mutex
	databaseFolder string
	client         sdm.SmartDeviceManagementServiceClient

	eventSubscribers map[string]func(event *Event)
	active           map[string]*Meta
}

func NewService(ctx context.Context, addr string, databaseFolder string,
	config *oauth2.Config, token *oauth2.Token) (*service, error) {

	conn, err := gtransport.Dial(ctx, option.WithEndpoint(addr), option.WithTokenSource(config.TokenSource(ctx, token)))
	if err != nil {
		return nil, fmt.Errorf("gtransport.Dial: %v", err)
	}

	client := sdm.NewSmartDeviceManagementServiceClient(conn)

	return &service{
		client:           client,
		databaseFolder:   databaseFolder,
		Mutex:            sync.Mutex{},
		active:           make(map[string]*Meta),
		eventSubscribers: make(map[string]func(*Event)),
	}, nil
}

func (s *service) GetDevices(ctx context.Context) ([]*sdm.Device, error) {
	res, err := s.client.ListDevices(ctx, &sdm.ListDevicesRequest{})
	if err != nil {
		return nil, err
	}
	return res.GetDevices(), nil
}

func (s *service) RequestRTSPURL(ctx context.Context, deviceName string) (string, error) {
	res, err := s.client.ExecuteDeviceCommand(ctx, &sdm.ExecuteDeviceCommandRequest{
		Name:    deviceName,
		Command: "sdm.devices.commands.CameraLiveStream.GenerateRtspStream",
	})
	if err != nil {
		return "", err
	}

	results := res.GetResults().AsMap()
	streamURLS, ok := results["streamUrls"]
	if !ok {
		return "", errors.New("no stream URLs returned")
	}

	rtspURL, ok := streamURLS.(map[string]interface{})["rtspUrl"]
	if !ok {
		return "", errors.New("no RTSP url returned")
	}

	return rtspURL.(string), nil

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
