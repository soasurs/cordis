package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/zrpc"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/observability"
	"github.com/soasurs/cordis/services/session/v1/config"
	"github.com/soasurs/cordis/services/session/v1/internal/server"
	"github.com/soasurs/cordis/services/session/v1/internal/svc"
)

var configPath = flag.String("c", "etc/config.yaml", "config file of service")

func main() {
	flag.Parse()

	cfg := new(config.Config)
	if err := conf.LoadConfig(*configPath, cfg, conf.UseEnv()); err != nil {
		panic(err)
	}
	svcCtx := svc.NewServiceContext(*cfg)
	defer svcCtx.Close()
	sessionServer := server.New(svcCtx)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	sessionServer.StartBackground(ctx)

	rpcConfig := cfg.RPCConfig()
	traceRPC := rpcConfig.Middlewares.Trace
	rpcConfig.Middlewares.Trace = false
	s, err := zrpc.NewServer(rpcConfig, func(grpcServer *grpc.Server) {
		sessionv1.RegisterSessionServiceServer(grpcServer, sessionServer)
	})
	if err != nil {
		panic(err)
	}
	if traceRPC {
		s.AddOptions(grpc.StatsHandler(otelgrpc.NewServerHandler(otelgrpc.WithFilter(
			observability.ExcludeGRPCMethods(sessionv1.SessionService_Connect_FullMethodName),
		))))
	}
	go func() {
		<-ctx.Done()
		drainCtx, cancel := context.WithTimeout(context.Background(), cfg.Node.DrainWindow()+5*time.Second)
		defer cancel()
		sessionServer.Drain(drainCtx)
		s.Stop()
	}()
	s.Start()
}
