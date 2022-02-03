package rtsp

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"

	"github.com/bilbercode/nest-stream/internal/rtsp/transport"
	"github.com/pion/sdp/v3"
)

type server struct {
	sync.Mutex
	basepath    *url.URL
	description *sdp.SessionDescription
	cameras     map[string]*camera
}

func NewServer(basepath *url.URL) Server {
	return &server{
		Mutex:    sync.Mutex{},
		basepath: basepath,
		cameras:  make(map[string]*camera),
		description: &sdp.SessionDescription{
			Version: 0,
			Origin: sdp.Origin{
				Username:       "-",
				SessionID:      0,
				SessionVersion: 0,
				NetworkType:    "IN",
				AddressType:    "IP4",
				UnicastAddress: "127.0.0.1",
			},
			SessionName: "front_door",
			ConnectionInformation: &sdp.ConnectionInformation{
				NetworkType: "IN",
				AddressType: "IP4",
				Address: &sdp.Address{
					Address: "0.0.0.0",
				},
			},
			TimeDescriptions: []sdp.TimeDescription{
				{
					Timing: sdp.Timing{},
				},
			},
			Attributes: []sdp.Attribute{
				{
					Key:   "sdplang",
					Value: "en",
				},
				{
					Key:   "range",
					Value: "ntp=now-",
				},
				{
					Key:   "control",
					Value: "*",
				},
			},
		},
	}
}

func (s *server) RegisterCamera(name string, d *sdp.MediaDescription) Camera {
	cam := NewCamera()
	cam.description = d
	s.Lock()
	defer s.Unlock()
	s.cameras[name] = cam
	return cam
}

func (s *server) Start(ctx context.Context, addr string) error {
	conf := net.ListenConfig{}
	listener, err := conf.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on address %s: %w", addr, err)
	}

	go func() {
		<-ctx.Done()
		listener.Close()
	}()
	for {
		nc, err := listener.Accept()
		switch {
		case err == context.Canceled:
			return nil
		case err != nil:
			return err
		default:
			go s.handle(ctx, nc)
		}
	}
}

func (s *server) handle(ctx context.Context, nc net.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	cli := NewClientWithContextCancel(nc, ctx, cancel)
	cli.SubscribeRequests(func(request *Request, cli Client) error {
		var err error
		switch request.Method {
		case MethodOptions:
			err = s.handleOptions(ctx, request, cli)
		case MethodDescribe:
			err = s.handleDescribe(ctx, request, cli)
		case MethodSetup:
			err = s.handleSetup(ctx, request, cli)
		case MethodPlay:
			err = s.handlePlay(ctx, request, cli)
		case MethodGetParameter:

			err = s.handleGetParameter(ctx, request, cli)
		case MethodTeardown:
			err = s.handleTeardown(ctx, request, cli)
		default:
			err = s.handleUnsupportedMethod(ctx, request, cli, false)
		}
		if err != nil {
			return fmt.Errorf("unexpected client error: %w", err)
		}

		return nil
	})

	<-cli.Done()
}

func (s *server) handleOptions(ctx context.Context, request *Request, cli Client) error {
	header := http.Header{
		"Public": []string{
			"DESCRIBE SETUP PLAY TEARDOWN GET_PARAMETER",
		},
	}
	cSeq := request.Header.Get("CSeq")
	return cli.SendResponse(ctx, &Response{
		Version:  "1.0",
		Code:     http.StatusOK,
		Message:  http.StatusText(http.StatusOK),
		Header:   header,
		Sequence: cSeq,
	})
}

func (s *server) handleDescribe(ctx context.Context, request *Request, cli Client) error {
	requestURL, err := url.Parse(request.Url)
	if err != nil {
		return s.returnError(ctx, nil, cli, request.Sequence, http.StatusBadRequest, "invalid URL provided")
	}

	if request.Header.Get("Accept") != "" && request.Header.Get("Accept") != "application/sdp" {
		return s.returnError(
			ctx,
			nil,
			cli,
			request.Sequence,
			http.StatusNotAcceptable,
			http.StatusText(http.StatusNotAcceptable),
		)
	}

	_, cameraURI := path.Split(requestURL.Path)
	s.Lock()
	camera, ok := s.cameras[cameraURI]
	s.Unlock()
	if !ok {
		return cli.SendResponse(ctx, &Response{
			Version:  "1.0",
			Code:     http.StatusNotFound,
			Message:  http.StatusText(http.StatusNotFound),
			Sequence: request.Sequence,
		})
	}

	description := *s.description
	description.MediaDescriptions = append(description.MediaDescriptions, camera.description)
	uri, _ := url.Parse("rtsp://127.0.0.1:8554/stream/front_door")
	description.URI = uri
	descRes, err := description.Marshal()
	if err != nil {
		return s.returnError(
			ctx,
			nil,
			cli,
			request.Sequence,
			http.StatusInternalServerError,
			http.StatusText(http.StatusInternalServerError),
		)
	}

	body := bytes.NewBuffer(descRes)

	header := http.Header{}
	header.Set("Content-Type", "application/sdp")
	header.Set("Content-Base", requestURL.String())
	return cli.SendResponse(ctx, &Response{
		Version:  "1.0",
		Code:     http.StatusOK,
		Message:  http.StatusText(http.StatusOK),
		Sequence: request.Sequence,
		Header:   header,
		Body:     body,
	})
}

func (s *server) handleSetup(ctx context.Context, request *Request, cli Client) error {
	requestURL, err := url.Parse(request.Url)
	if err != nil {
		return s.returnError(ctx, nil, cli, request.Sequence, http.StatusBadRequest, "invalid URL provided")
	}
	cameraURI, _ := path.Split(requestURL.Path)
	_, cameraURI = path.Split(strings.TrimSuffix(cameraURI, "/"))
	s.Lock()
	camera, ok := s.cameras[cameraURI]
	s.Unlock()
	if !ok {
		return cli.SendResponse(ctx, &Response{
			Version:  "1.0",
			Code:     http.StatusNotFound,
			Message:  http.StatusText(http.StatusNotFound),
			Sequence: request.Sequence,
		})
	}

	if request.Header.Get("Transport") != "" {
		ts, err := transport.Parse(request.Header[http.CanonicalHeaderKey("Transport")])
		switch {
		case err == transport.ErrUnsupportedTransport:
			return cli.SendResponse(ctx, &Response{
				Version:  "1.0",
				Code:     transport.UnsupportedTransportCode,
				Message:  transport.UnsupportedTransportMessage,
				Sequence: request.Sequence,
			})
		case err != nil:
			return cli.SendResponse(ctx, &Response{
				Version:  "1.0",
				Code:     http.StatusBadRequest,
				Message:  http.StatusText(http.StatusBadRequest),
				Sequence: request.Sequence,
			})
		}

		ssrc := uint32(0)
		lastRTPTime := time.Now()
		lastRTPTimeRPT := uint32(0)
		sent := uint32(0)
		oc := uint32(0)

		camera.RegisterSubscriber(
			"3984798345",
			func(packet *rtp.Packet) {
				ssrc = packet.SSRC
				lastRTPTimeRPT = packet.Timestamp
				lastRTPTime = time.Now()
				payload := []byte{0x24, 0, 0x00, 0x00}
				b, err := packet.Marshal()
				if err == nil {
					binary.BigEndian.PutUint16(payload[2:], uint16(len(b)))
				}
				payload = append(payload, b...)
				_, err = cli.Conn().Write(payload)
				if err != nil {
					fmt.Println("write error")
				}
				sent++
				oc += uint32(len(b))
			},
			func(packet rtcp.Packet) {
				payload := []byte{0x24, 1, 0x00, 0x00}
				b, err := packet.Marshal()
				if err != nil {
					binary.BigEndian.PutUint16(payload[:2], uint16(len(b)))
				}
				payload = append(payload, b...)
				cli.Conn().Write(payload)
			},
		)

		header := http.Header{}
		header.Set("Session", "3984798345")
		header.Set("Transport", ts.Options()[0].String())

		return cli.SendResponse(ctx, &Response{
			Version:  "1.0",
			Code:     http.StatusOK,
			Message:  http.StatusText(http.StatusOK),
			Sequence: request.Sequence,
			Header:   header,
		})

	}

	return cli.SendResponse(ctx, &Response{
		Version:  "1.0",
		Code:     461,
		Message:  "Unsupported Transport",
		Sequence: request.Sequence,
	})
}

func (s *server) handlePlay(ctx context.Context, request *Request, cli Client) error {
	requestURL, err := url.Parse(request.Url)
	if err != nil {
		return s.returnError(ctx, nil, cli, request.Sequence, http.StatusBadRequest, "invalid URL provided")
	}
	_, cameraURI := path.Split(requestURL.Path)
	if request.Header.Get("Session") != "" {

		s.Lock()
		camera, ok := s.cameras[cameraURI]
		s.Unlock()
		if !ok {
			return cli.SendResponse(ctx, &Response{
				Version:  "1.0",
				Code:     http.StatusNotFound,
				Message:  http.StatusText(http.StatusNotFound),
				Sequence: request.Sequence,
			})
		}

		camera.Play(request.Header.Get("Session"))
		header := http.Header{}
		header.Set("Session", request.Header.Get("Session"))
		header.Set("Range", fmt.Sprintf("ntp:%s-", strconv.FormatFloat(time.Duration(0).Seconds(), 'f', -1, 64)))

		return cli.SendResponse(ctx, &Response{
			Version:  "1.0",
			Code:     http.StatusOK,
			Message:  http.StatusText(http.StatusOK),
			Sequence: request.Sequence,
			Header:   header,
		})
	}
	return cli.SendResponse(ctx, &Response{
		Version:  "1.0",
		Code:     454,
		Message:  "Session Not Found",
		Sequence: request.Sequence,
	})
}

func (s *server) handleGetParameter(ctx context.Context, request *Request, cli Client) error {
	header := http.Header{}
	header.Set("Session", request.Header.Get("Session"))
	return cli.SendResponse(ctx, &Response{
		Version:  "1.0",
		Code:     http.StatusOK,
		Message:  http.StatusText(http.StatusOK),
		Sequence: request.Sequence,
		Header:   header,
	})
}

func (s *server) handleTeardown(ctx context.Context, request *Request, cli Client) error {
	requestURL, err := url.Parse(request.Url)
	if err != nil {
		return s.returnError(ctx, nil, cli, request.Sequence, http.StatusBadRequest, "invalid URL provided")
	}
	_, cameraURI := path.Split(requestURL.Path)
	if request.Header.Get("Session") != "" {
		s.Lock()
		camera, ok := s.cameras[cameraURI]
		s.Unlock()
		if !ok {
			return cli.SendResponse(ctx, &Response{
				Version:  "1.0",
				Code:     http.StatusNotFound,
				Message:  http.StatusText(http.StatusNotFound),
				Sequence: request.Sequence,
			})
		}
		camera.Teardown(request.Header.Get("Session"))
		return cli.SendResponse(ctx, &Response{
			Version:  "1.0",
			Code:     http.StatusOK,
			Message:  http.StatusText(http.StatusOK),
			Sequence: request.Sequence,
		})
	}
	return cli.SendResponse(ctx, &Response{
		Version:  "1.0",
		Code:     454,
		Message:  "Session Not Found",
		Sequence: request.Sequence,
	})
}

func (s *server) handleUnsupportedMethod(ctx context.Context, request *Request, se Client, includeAnnounce bool) error {

	options := []string{
		MethodDescribe.String(),
		MethodGetParameter.String(),
		MethodSetup.String(),
		MethodPlay.String(),
		MethodTeardown.String(),
	}

	if includeAnnounce {
		options = append(options, MethodAnnounce.String())
	}

	cSeq := request.Header.Get("CSeq")

	header := http.Header{}
	header.Add("Allow", strings.Join(options, ", "))

	return s.returnError(ctx, header, se, cSeq, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
}

func (s *server) returnError(ctx context.Context, headers map[string][]string, se Client, seq string, code int, message string) error {
	return se.SendResponse(ctx, &Response{
		Version:  "1.0",
		Code:     code,
		Message:  message,
		Sequence: seq,
		Header:   headers,
	})
}
