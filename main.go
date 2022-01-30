package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/bilbercode/nest-stream/internal/api"
	"github.com/bilbercode/nest-stream/internal/swagger"
	"github.com/utilitywarehouse/swaggerui"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/bilbercode/nest-stream/internal/camera"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_logrus "github.com/grpc-ecosystem/go-grpc-middleware/logging/logrus"
	grpc_recovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/bilbercode/nest-stream/pkg/nest_stream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"

	"github.com/bilbercode/nest-stream/internal/rtsp"

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

func main() {

	app := cli.App(appName, appDesc)

	grpcPort := app.Int(cli.IntOpt{
		Name:   "grpc-port",
		Desc:   "grpc service port",
		EnvVar: "GRPC_PORT",
		Value:  8090,
	})

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

	database := app.String(cli.StringOpt{
		Name:   "database",
		Desc:   "database folder location",
		EnvVar: "DATABASE",
		Value:  "data/devices",
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

		grpcAPI := api.NewGRPCAPI()

		rtspServer := rtsp.NewServer(rtspBase)

		group, ctx := errgroup.WithContext(ctx)

		group.Go(func() error {
			return rtspServer.Start(ctx, fmt.Sprintf(":%s", rtspBase.Port()))
		})

		group.Go(func() error {

			var client *http.Client
			select {
			case <-ctx.Done():
				return ctx.Err()
			case client = <-authManager.GetClient():
			}

			deviceClient, err := sdm.NewService(client, *database)
			if err != nil {
				log.WithError(err).Panic("failed to start SDM service")
			}
			log.Info("client received from auth manager, creating camera proxy service")

			cameraService := camera.NewService(deviceClient, rtspServer, *rtspBaseURL)

			group.Go(func() error {
				return cameraService.Start(ctx)
			})

			grpcAPI.SetSDMService(deviceClient, *projectID)
			log.Info("GRPc API Ready")

			return nil
		})

		grpcServer := initialiseGRPCServer(grpcAPI)
		startGRPCServer(grpcServer, grpcPort)
		defer grpcServer.GracefulStop()

		authManager.GetMux().Handle("/__/metrics", promhttp.Handler())

		initialiseSwaggerAPI(ctx, authManager.GetMux(), grpcPort)

		group.Go(func() error {
			log.Info("Starting Auth manager")
			return authManager.Start(ctx)
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

func initialiseSwaggerAPI(ctx context.Context, mux *http.ServeMux, grpcPort *int) {

	gwmux := runtime.NewServeMux(runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
		MarshalOptions: protojson.MarshalOptions{
			UseProtoNames:   true,
			EmitUnpopulated: true,
		},
		UnmarshalOptions: protojson.UnmarshalOptions{},
	}))

	mux.Handle("/", gwmux)
	mux.Handle("/swagger-ui/", http.StripPrefix("/swagger-ui/", swaggerui.SwaggerUI()))
	mux.Handle("/swagger.json", http.FileServer(http.FS(swagger.Static)))

	dialOpts := []grpc.DialOption{grpc.WithInsecure()}
	err := nest_stream.RegisterCameraServiceHandlerFromEndpoint(ctx, gwmux, fmt.Sprintf("localhost:%d", *grpcPort), dialOpts)
	if err != nil {
		log.WithError(err).Panic("unable to register gateway handler")
	}
}

func initialiseGRPCServer(svc nest_stream.CameraServiceServer) *grpc.Server {
	grpc_prometheus.EnableHandlingTimeHistogram()

	logger := log.New()
	logger.SetLevel(log.WarnLevel)
	grpc_logrus.ReplaceGrpcLogger(log.NewEntry(logger))
	logOpts := []grpc_logrus.Option{
		grpc_logrus.WithDecider(func(methodFullName string, err error) bool {
			if err == nil && methodFullName == "/grpc.health.v1.Health/Check" {
				return false
			}

			// by default you will log all calls
			return true
		}),
	}
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
			grpc_prometheus.UnaryServerInterceptor,
			grpc_recovery.UnaryServerInterceptor(),
			grpc_logrus.UnaryServerInterceptor(log.NewEntry(log.StandardLogger()), logOpts...),
		)),
	)

	nest_stream.RegisterCameraServiceServer(grpcServer, svc)
	healthpb.RegisterHealthServer(grpcServer, health.NewServer())

	return grpcServer
}

func startGRPCServer(grpcServer *grpc.Server, grpcPort *int) {
	listen, err := net.Listen("tcp", fmt.Sprintf(":%d", *grpcPort))
	if err != nil {
		log.WithField("port", grpcPort).WithError(err).Panic("could not listen on GRPC port")
	}

	go func() {
		if err := grpcServer.Serve(listen); err != nil {
			log.WithError(err).Panic("could not serve GRPC connections")
		}
	}()
}
