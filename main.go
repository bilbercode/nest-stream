package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bilbercode/nest-stream/internal/rtsp"

	"github.com/pion/rtp"

	"github.com/pion/sdp/v3"

	"github.com/bilbercode/nest-stream/internal/sdm"

	"github.com/bilbercode/nest-stream/internal/auth"
	"golang.org/x/sync/errgroup"

	cli "github.com/jawher/mow.cli"
	log "github.com/sirupsen/logrus"
)

const (
	appName = "nest-stream"
	appDesc = "nest camera proxy"
)

var previousSessionID string

func main() {

	app := cli.App(appName, appDesc)

	httpBaseURL := app.String(cli.StringOpt{
		Name:   "url.http",
		Desc:   "base URL for the application",
		EnvVar: "BASE_URL_RSTP",
		Value:  "http://localhost:8080",
	})

	rtspBaseURL := app.String(cli.StringOpt{
		Name:   "url.rtsp",
		Desc:   "base URL for the application",
		EnvVar: "BASE_URL_RTSP",
		Value:  "rtsp://localhost:8554",
	})

	projectID := app.String(cli.StringOpt{
		Name:   "project-id",
		Desc:   "google project ID",
		EnvVar: "PROJECT_ID",
		Value:  "",
	})

	credentialsLocation := app.String(cli.StringOpt{
		Name:   "credentials",
		Desc:   "credentials.json location",
		EnvVar: "CREDENTIALS_LOCATION",
		Value:  "credentials.json",
	})

	tokenLocation := app.String(cli.StringOpt{
		Name:   "token",
		Desc:   "token location",
		EnvVar: "TOKEN_LOCATION",
		Value:  "data/token",
	})

	app.Action = func() {
		ctx := context.Background()

		authManager, err := auth.NewManager(*projectID, *credentialsLocation, *tokenLocation, *httpBaseURL)
		if err != nil {
			log.WithError(err).Panic("failed to create authentication manager")
		}

		rtspBase, err := url.Parse(*rtspBaseURL)
		if err != nil {
			log.WithError(err).Panic("failed to parse RTSP base url")
		}

		rtspServer := rtsp.NewServer(rtspBase)

		group, ctx := errgroup.WithContext(ctx)

		group.Go(func() error {
			return authManager.Start(ctx)
		})

		group.Go(func() error {
			return rtspServer.Start(ctx, fmt.Sprintf(":%s", rtspBase.Port()))
		})

		group.Go(func() error {
			log.Info("waiting for client from auth manager")

			var client *http.Client
			select {
			case <-ctx.Done():
				return ctx.Err()
			case client = <-authManager.GetClient():
			}

			deviceClient := sdm.NewService(client)
			log.Info("client received, querying for SDM devices")

			devices, err := deviceClient.GetDevices(ctx)
			if err != nil {
				return fmt.Errorf("failed to query google SDM for devices: %w", err)
			}
			// TODO (bilbercode) this should be configurable from GUI
			// TODO (GUI)

			// Start a new conference for each device
			log.Info("Generating persistent conference for front_door")

			var (
				previousConnCtxCancel context.CancelFunc
				camera                rtsp.Camera
				token                 *sdm.CommandResponseGenerateRTSPStream
			)

			retryCount := 0
			for {
			getToken:
				log.Info("Requesting new stream location from Google")

				token, err = deviceClient.GenerateRTSPStream(ctx, devices[0])
				switch {
				case retryCount < 5 && err != nil:
					log.Info("failed to get token from google, retrying")
					retryCount++
					goto getToken
				case err != nil:
					log.Info("failed to get token from google, bailing")
					return err
				case token == nil:
					log.Info("failed to get token from google, retrying")
					retryCount++
					goto getToken
				default:
					retryCount = 0
				}

				for _, tokenURI := range token.URLS {
					log.Info("Joining new stream to front_door conference")
					streamCtx, cam, _, err := joinURLToConference(ctx, tokenURI, rtspServer, camera)
					switch {
					case retryCount < 5 && err != nil:
						log.Info("stream failure, restarting")
						retryCount++
						goto getToken
					case err != nil:
						log.Info("stream failure, bailing")
						return err
					default:
						log.Info("Joined")
						retryCount = 0
					}

					if previousConnCtxCancel != nil {
						previousConnCtxCancel()
					}

					camera = cam
					select {
					case <-streamCtx.Done():
						break
					case <-ctx.Done():
						return nil
					}
				}

			}
		})

		err = group.Wait()
		if err != nil {
			log.WithError(err).Panic("stopped")
		}
	}

	err := app.Run(os.Args)
	if err != nil {
		log.WithError(err).Panic("failed to execute application")
	}
}

func joinURLToConference(ctx context.Context, addr string, server rtsp.Server, camera rtsp.Camera) (context.Context, rtsp.Camera, context.CancelFunc, error) {
	seq := int64(1)
	uri, err := url.Parse(addr)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	dialer := &net.Dialer{}
	nc, err := dialer.DialContext(ctx, "tcp", uri.Host)
	if err != nil {
		cancel()
		return nil, nil, nil, fmt.Errorf("failed to dial endpoint %s: %w", uri.Host, err)
	}

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
		return nil, nil, nil, fmt.Errorf("failed to get session description for url %s: %w", uri.String(), err)
	}
	seq++

	sessionDescription := &sdp.SessionDescription{}
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to copy resposne body for Describe on URL %s: %w", uri.String(), err)
	}
	err = sessionDescription.Unmarshal(b)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to parse SDP for URL %s: %w", uri.String(), err)
	}

	base := res.Header.Get("Content-Base")
	trackID := 0

	rtpChan := make(chan *rtp.Packet)
	var sessionID string
	var mediaDescription *sdp.MediaDescription
	for i, md := range sessionDescription.MediaDescriptions {
		if i == 0 {
			continue
		}
		mediaDescription = md
		client := rtspClient

		control, ok := md.Attribute("control")
		if !ok {
			return nil, nil, nil, errors.New("SDP failed to return control attribute ")
		}

		// Setup
		header = http.Header{
			"Transport": []string{
				fmt.Sprintf("AVP/RTP/TCP;unicast;interleaved=%d-%d", trackID*2, (trackID*2)+1),
			},
		}

		res, err = client.SendRequest(ctx, &rtsp.Request{
			Version:  "1.0",
			Url:      path.Join(base, control),
			Sequence: strconv.FormatInt(seq, 10),
			Method:   rtsp.MethodSetup,
			Header:   header,
		})

		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed setup resources on URL %s: %w", path.Join(base, "trackID=1"), err)
		}
		seq++

		sessionID = strings.Split(res.Header.Get("Session"), ";")[0]
		if sessionID == "" {
			return nil, nil, nil, errors.New("no session ID returned")
		}

		if previousSessionID == sessionID {
			log.Error("FFS google")
		}
		previousSessionID = sessionID

		if camera == nil {
			camera = server.RegisterCamera("front_door", mediaDescription)
		}

		go camera.MergeStream(rtpChan, sessionID)

		var timer *time.Timer

		once := sync.Once{}
		client.SubscribeInterleavedFrames(func(channel uint8, payload []byte) {
			if channel == 0 {
				if timer != nil {
					timer.Reset(time.Second * 10)
				}
				once.Do(func() {
					go func() {
						timer = time.NewTimer(time.Second * 30)
						for {
							select {
							case <-ctx.Done():
								return
							case <-timer.C:
								close(rtpChan)
								cancel()
							}
						}
					}()
				})
				packet := &rtp.Packet{}
				err := packet.Unmarshal(payload)
				if err == nil {
					select {
					case <-ctx.Done():
						log.Info("Context Done")
						return
					case rtpChan <- packet:
					}
				}
			}
		})

		trackID++
	}

	header = http.Header{}
	header.Set("Session", sessionID)

	res, err = rtspClient.SendRequest(ctx, &rtsp.Request{
		Version:  "1.0",
		Url:      base,
		Method:   rtsp.MethodPlay,
		Header:   header,
		Sequence: strconv.FormatInt(seq, 10),
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get session description for url %s: %w", uri.String(), err)
	}

	seq++
	go func() {
		timer := time.NewTimer(time.Second * 30)
		for {
			select {
			case <-rtspClient.Done():
			case <-timer.C:
				res, err = rtspClient.SendRequest(ctx, &rtsp.Request{
					Version:  "1.0",
					Url:      addr,
					Method:   rtsp.MethodGetParameter,
					Sequence: strconv.FormatInt(seq, 10),
					Header:   header,
				})

				if err != nil {
					cancel()
				}
				if res == nil || ctx.Err() != nil {
					return
				}
				seq++
				timer.Reset(time.Second * 30)
			}
		}
	}()

	return ctx, camera, cancel, nil
}
