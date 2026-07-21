package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/proc"
	"github.com/zeromicro/go-zero/zrpc"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/observability"
	"github.com/soasurs/cordis/pkg/probe"
	"github.com/soasurs/cordis/services/session/v1/config"
	"github.com/soasurs/cordis/services/session/v1/internal/server"
	"github.com/soasurs/cordis/services/session/v1/internal/svc"
)

var configPath = flag.String("c", "etc/config.yaml", "config file of service")

type startupDrainCoordinator struct {
	startup chan bool
	done    chan struct{}
}

func newStartupDrainCoordinator() *startupDrainCoordinator {
	return &startupDrainCoordinator{
		startup: make(chan bool, 1),
		done:    make(chan struct{}),
	}
}

func (c *startupDrainCoordinator) registered(ctx context.Context, markReady func()) {
	if ctx.Err() == nil {
		markReady()
	}
	c.startup <- true
}

func (c *startupDrainCoordinator) abort() {
	c.startup <- false
}

func (c *startupDrainCoordinator) drainOnCancel(ctx context.Context, drain func()) {
	defer close(c.done)
	<-ctx.Done()
	if <-c.startup {
		drain()
	}
}

func (c *startupDrainCoordinator) wait() {
	<-c.done
}

func main() {
	flag.Parse()

	cfg := new(config.Config)
	if err := conf.LoadConfig(*configPath, cfg, conf.UseEnv()); err != nil {
		panic(err)
	}
	proc.SetTimeToForceQuit(cfg.Node.ShutdownTimeout())
	svcCtx := svc.NewServiceContext(*cfg)
	defer svcCtx.Close()
	sessionServer := server.New(svcCtx)
	probeState := probe.New()
	probeState.SetLiveness(true)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	sessionServer.StartBackground(ctx)

	rpcConfig := cfg.RPCConfig()
	traceRPC := rpcConfig.Middlewares.Trace
	rpcConfig.Middlewares.Trace = false
	startup := newStartupDrainCoordinator()
	s, err := zrpc.NewServer(rpcConfig, func(grpcServer *grpc.Server) {
		sessionv1.RegisterSessionServiceServer(grpcServer, sessionServer)
		probeState.RegisterGRPC(grpcServer)
		startup.registered(ctx, func() {
			probeState.SetReadiness(true)
		})
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
		startup.drainOnCancel(ctx, func() {
			probeState.SetReadiness(false)
			drainCtx, cancel := context.WithTimeout(context.Background(), cfg.Node.DrainWindow()+5*time.Second)
			defer cancel()
			sessionServer.Drain(drainCtx)
		})
	}()
	if ctx.Err() != nil {
		startup.abort()
		startup.wait()
		return
	}
	s.Start()
	if ctx.Err() != nil {
		startup.wait()
	}
}
