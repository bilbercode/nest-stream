package camera

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/rtcp"

	"github.com/bilbercode/nest-stream/internal/devices"

	log "github.com/sirupsen/logrus"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"golang.org/x/sync/errgroup"

	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"

	"github.com/bilbercode/nest-stream/internal/rtsp"
)

var (
	cameraErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name:      "camera_errors",
		Namespace: "nest_stream",
		Help:      "number of errors the camera has encountered",
	}, []string{"camera"})
	cameraRestarts = promauto.NewCounterVec(prometheus.CounterOpts{
		Name:      "camera_restarts",
		Namespace: "nest_stream",
		Help:      "number of camera restarts",
	}, []string{"camera"})
	sdmErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name:      "sdm_errors",
		Namespace: "nest_stream",
		Help:      "number of errors from SDM stream",
	}, []string{"code", "type"})
)

type service struct {
	rtsp     rtsp.Server
	sdm      devices.Service
	basePath string
	cancel   context.CancelFunc
}

func NewService(sdm devices.Service, rtsp rtsp.Server, basePath string) Service {
	return &service{sdm: sdm, rtsp: rtsp, basePath: basePath}
}

func (s *service) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)
	ec := make(chan error)
	group, ctx := errgroup.WithContext(ctx)
	var deregister func()
	group.Go(func() error {
		deregister = s.sdm.Subscribe(func(event *devices.Event) {
			if event.Type == devices.EventTypeStartCamera {
				log.Infof("camera `%s` start received from SDM service", event.Meta.Name)
				err := s.StartProxy(ctx, event.Meta)

				if err != nil {
					select {
					case <-ctx.Done():
					case ec <- err:
					}
				}
			}
		})
		return nil
	})

	group.Go(func() error {
		err := <-ec
		s.cancel()
		close(ec)
		if err != nil {
			return err
		}
		return nil
	})

	return group.Wait()
}

func (s *service) Close() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *service) StartProxy(ctx context.Context, camera *devices.Meta) error {

	var media *sdp.MediaDescription
	var conference rtsp.Camera
	for {
		log.Infof("querying SDM for new RTSPs stream URL for camera %s", camera.Name)
		addr, err := s.sdm.RequestRTSPURL(ctx, camera.Id)
		switch {
		case err != nil:
			log.Error(err)
			sdmErrors.WithLabelValues("500", "internal")
			return err
		}
		log.Infof("SDM returned RTSP target for camera %s", camera.Name)

		if media == nil {
			log.Infof("Querying SDP from RTSP server for camera %s", camera.Name)
			media, err = s.GetVideoMediaDescription(ctx, addr)
			if err != nil {
				return err
			}
			log.Infof("Registering camera feed with RTSP service for camera %s", camera.Name)
			conference = s.rtsp.RegisterCamera(camera.Name, media)
			log.Infof("camera feed %s is now available at `%s`", camera.Name, path.Join(s.basePath, camera.Name))
		}
		err = s.streamToCompletion(ctx, addr, media, conference)
		if err != nil {
			<-time.After(time.Second * 2)
			cameraErrors.WithLabelValues(camera.Name).Inc()
			log.WithError(err).Warn("camera stream error")
			continue
		}
		log.Infof("camera stream %s completed section, moving to next interval", camera.Name)
		cameraRestarts.WithLabelValues(camera.Name).Inc()
	}
}

func (s *service) streamToCompletion(ctx context.Context, addr string, media *sdp.MediaDescription, conference rtsp.Camera) error {

	seq := int64(1)
	uri, err := url.Parse(addr)
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	dialer := &net.Dialer{}
	nc, err := dialer.DialContext(ctx, "tcp", uri.Host)
	if err != nil {
		cancel()
		return fmt.Errorf("failed to dial endpoint %s: %w", uri.Host, err)
	}

	defer nc.Close()
	if uri.Scheme == "rtsps" {
		config := &tls.Config{
			ServerName: uri.Hostname(),
		}
		nc = tls.Client(nc, config)
	}

	rtspClient := rtsp.NewClientWithContextCancel(nc, ctx, cancel)

	res, err := rtspClient.SendRequest(ctx, &rtsp.Request{
		Version:  "1.0",
		Url:      uri.String(),
		Sequence: strconv.FormatInt(seq, 10),
		Method:   rtsp.MethodDescribe,
	})

	if err != nil {
		return fmt.Errorf("failed to get session description for url %s: %w", uri.String(), err)
	}

	sessionID := strings.Split(res.Header.Get("Session"), ";")[0]
	if sessionID == "" {
		return errors.New("no session ID returned")
	}

	uri, _ = url.Parse("Content-Base")

	seq++

	header := http.Header{
		"Transport": []string{
			"AVP/RTP/TCP;unicast;interleaved=0-1",
		},
		"Session": []string{sessionID},
	}
	control, _ := media.Attribute("control")
	uri.Path = path.Join(uri.Path, control)
	res, err = rtspClient.SendRequest(ctx, &rtsp.Request{
		Version:  "1.0",
		Url:      uri.String(),
		Sequence: strconv.FormatInt(seq, 10),
		Method:   rtsp.MethodSetup,
		Header:   header,
	})

	if err != nil {
		return fmt.Errorf("failed setup resources on URL %s: %w", uri.String(), err)
	}
	seq++

	uri.Path = strings.Replace(uri.Path, control, "", 1)
	uri.RawQuery = ""
	var timer *time.Timer

	once := sync.Once{}
	rtpChan := make(chan *rtp.Packet)
	il := sync.Mutex{}
	closing := false
	rtspClient.SubscribeInterleavedFrames(func(channel uint8, payload []byte) {
		if channel == 0 {
			il.Lock()
			if timer != nil {
				timer.Reset(time.Second * 10)
			}
			once.Do(func() {
				go func(cancel context.CancelFunc, nc net.Conn) {
					defer close(rtpChan)
					timer = time.NewTimer(time.Second * 30)
					for {
						select {
						case <-ctx.Done():
							return
						case <-timer.C:
							il.Lock()
							cancel()
							closing = true
							il.Unlock()
							return
						}
					}
				}(cancel, nc)
			})
			packet := &rtp.Packet{}
			err := packet.Unmarshal(payload)
			if err == nil && !closing {
				select {
				case <-ctx.Done():
					return
				case rtpChan <- packet:
				}
			}
			il.Unlock()
		} else {
			packets, err := rtcp.Unmarshal(payload)
			if err == nil && !closing {
				fmt.Println(packets)
			}
		}
	})

	header = http.Header{}
	header.Set("Session", sessionID)

	res, err = rtspClient.SendRequest(ctx, &rtsp.Request{
		Version:  "1.0",
		Url:      uri.String(),
		Method:   rtsp.MethodPlay,
		Header:   header,
		Sequence: strconv.FormatInt(seq, 10),
	})
	if err != nil {
		return fmt.Errorf("failed to request server to start stream %s: %w", uri.String(), err)
	}
	seq++
	go func(cancelFunc context.CancelFunc, nc net.Conn) {
		timer := time.NewTimer(time.Second * 30)
		for {
			select {
			case <-rtspClient.Done():
				timer.Stop()
				return
			case <-timer.C:
				res, err = rtspClient.SendRequest(ctx, &rtsp.Request{
					Version:  "1.0",
					Url:      uri.String(),
					Method:   rtsp.MethodGetParameter,
					Sequence: strconv.FormatInt(seq, 10),
					Header:   header,
				})

				if err != nil {
					cancelFunc()
					return
				}
				if res == nil || ctx.Err() != nil {
					return
				}
				seq++
				timer.Reset(time.Second * 30)
			}
		}
	}(cancel, nc)

	conference.MergeStream(rtpChan, sessionID)

	return nil
}

func (s *service) GetVideoMediaDescription(ctx context.Context, addr string) (*sdp.MediaDescription, error) {
	seq := int64(1)
	uri, err := url.Parse(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	dialer := &net.Dialer{}
	nc, err := dialer.DialContext(ctx, "tcp", uri.Host)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to dial endpoint %s: %w", uri.Host, err)
	}
	defer nc.Close()

	if uri.Scheme == "rtsps" {
		config := &tls.Config{
			ServerName: uri.Hostname(),
		}
		nc = tls.Client(nc, config)
	}

	rtspClient := rtsp.NewClientWithContextCancel(nc, ctx, cancel)

	header := http.Header{
		"Accept": []string{"application/sdp"},
	}
	res, err := rtspClient.SendRequest(ctx, &rtsp.Request{
		Version:  "1.0",
		Url:      uri.String(),
		Sequence: strconv.FormatInt(seq, 10),
		Method:   rtsp.MethodDescribe,
		Header:   header,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get session description for url %s: %w", uri.String(), err)
	}
	seq++

	sessionDescription := &sdp.SessionDescription{}
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to copy resposne body for Describe on URL %s: %w", uri.String(), err)
	}
	err = sessionDescription.Unmarshal(b)
	if err != nil {
		return nil, fmt.Errorf("failed to parse SDP for URL %s: %w", uri.String(), err)
	}
	for _, md := range sessionDescription.MediaDescriptions {

		rtpmap, ok := md.Attribute("rtpmap")
		switch {
		case !ok:
			continue
		case strings.Contains(rtpmap, "H264"):
			return md, nil
		}
	}

	return nil, errors.New("no valid H256 stream described by server")
}
