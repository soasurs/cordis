package main

import (
	"context"
	"errors"
	"flag"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/soasurs/cordis/pkg/observability"
	"github.com/soasurs/cordis/pkg/probe"
	"github.com/soasurs/cordis/services/gateway/v1/config"
	"github.com/soasurs/cordis/services/gateway/v1/internal/server"
	"github.com/soasurs/cordis/services/gateway/v1/internal/svc"
)

var configPath = flag.String("c", "etc/config.yaml", "config file of service")

func main() {
	flag.Parse()

	cfg := new(config.Config)
	if err := conf.LoadConfig(*configPath, cfg, conf.UseEnv()); err != nil {
		panic(err)
	}
	cfg.Log.ServiceName = cfg.Name
	logx.MustSetup(cfg.Log)
	defer logx.Close()
	observability.StartTracing(cfg.Name, cfg.Telemetry)
	defer observability.StopTracing()

	svcCtx := svc.NewServiceContext(*cfg)
	defer svcCtx.Close()
	gateway := server.New(svcCtx)
	defer gateway.Close()
	probeState := probe.New()
	probeServer, err := probe.StartHTTP(cfg.ProbeServer, probeState)
	if err != nil {
		panic(err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := probeServer.Shutdown(shutdownCtx); err != nil {
			logx.Errorw("shutdown gateway probe server", logx.Field("error", err))
		}
	}()

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	backgroundCtx, cancelBackground := context.WithCancel(context.Background())
	defer cancelBackground()
	gateway.StartBackground(backgroundCtx)

	httpServer := &http.Server{
		Addr:              cfg.ListenOn,
		Handler:           gateway.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	listener, err := net.Listen("tcp", cfg.ListenOn)
	if err != nil {
		panic(err)
	}
	probeState.SetLiveness(true)

	logx.Infow("gateway server listening",
		logx.Field("listen_on", cfg.ListenOn),
		logx.Field("websocket_path", cfg.Gateway.WebSocketRoute()),
	)
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- httpServer.Serve(listener)
	}()
	probeState.SetReadiness(true)
	select {
	case err := <-serveErr:
		probeState.SetReadiness(false)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	case <-signalCtx.Done():
		probeState.SetReadiness(false)
		httpShutdownCtx, httpCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer httpCancel()
		if err := httpServer.Shutdown(httpShutdownCtx); err != nil {
			logx.Errorw("shutdown gateway server", logx.Field("error", err))
			if closeErr := httpServer.Close(); closeErr != nil {
				logx.Errorw("force close gateway server", logx.Field("error", closeErr))
			}
		}
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer drainCancel()
		if err := gateway.Drain(drainCtx); err != nil {
			logx.Errorw("drain gateway connections", logx.Field("error", err))
		}
		cancelBackground()
		if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			logx.Errorw("serve gateway server", logx.Field("error", err))
		}
	}
}
