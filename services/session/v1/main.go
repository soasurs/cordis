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
	"google.golang.org/grpc"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
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
	sessionServer := server.New(svcCtx)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	sessionServer.StartBackground(ctx)

	s, err := zrpc.NewServer(cfg.RPCConfig(), func(grpcServer *grpc.Server) {
		sessionv1.RegisterSessionServiceServer(grpcServer, sessionServer)
	})
	if err != nil {
		panic(err)
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
