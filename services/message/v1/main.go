package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/services/message/v1/config"
	"github.com/soasurs/cordis/services/message/v1/internal/server"
	"github.com/soasurs/cordis/services/message/v1/internal/svc"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

var configPath = flag.String("c", "etc/config.yaml", "config file of service")

func main() {
	flag.Parse()

	cfg := new(config.Config)
	if err := conf.LoadConfig(*configPath, cfg, conf.UseEnv()); err != nil {
		panic(err)
	}

	svcCtx := svc.NewServiceContext(*cfg)
	srv := server.New(svcCtx)

	// Start the outbox relay (background poller) if Kafka is configured.
	// It recovers stale events that were not flushed immediately after commit.
	if svcCtx.Relay != nil {
		svcCtx.Relay.Start(context.Background())
	}

	zrpcSrv, err := zrpc.NewServer(cfg.RpcServerConf, func(grpcServer *grpc.Server) {
		messagev1.RegisterMessageServiceServer(grpcServer, srv)
	})
	if err != nil {
		panic(err)
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		slog.Info("received signal, shutting down", "signal", sig)

		// 1. Stop the gRPC server — no new requests.
		zrpcSrv.Stop()

		// 2. Stop the outbox relay — no new polling.
		if svcCtx.Relay != nil {
			svcCtx.Relay.Stop()
		}

		// 3. Wait for in-flight flush goroutines and relay operations
		// to finish (with a timeout).
		done := make(chan struct{})
		go func() {
			svcCtx.ShutdownWg.Wait()
			close(done)
		}()
		select {
		case <-done:
			slog.Info("all outbox operations completed")
		case <-time.After(10 * time.Second):
			slog.Warn("timeout waiting for outbox operations")
		}

		// 4. Close Kafka producer — flushes/fails all remaining records.
		if svcCtx.Kafka != nil {
			svcCtx.Kafka.Close()
		}

		// 5. gRPC server and database connections are cleaned up by
		// go-zero on process exit.
	}()

	slog.Info("starting message service", "listenOn", cfg.ListenOn)
	zrpcSrv.Start()
}
