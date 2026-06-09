package main

import (
	"context"
	"flag"
	"log/slog"

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/services/message/v1/config"
	"github.com/soasurs/cordis/services/message/v1/internal/server"
	"github.com/soasurs/cordis/services/message/v1/internal/svc"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/proc"
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

	deps, err := svc.NewDependencies(*cfg)
	if err != nil {
		panic(err)
	}
	svcCtx := svc.NewServiceContextWithDependencies(*cfg, deps)
	srv := server.New(svcCtx)

	// Start the outbox relay if Kafka is configured.
	if svcCtx.Relay != nil {
		svcCtx.Relay.Start(context.Background())
	}

	zrpcSrv, err := zrpc.NewServer(cfg.RpcServerConf, func(grpcServer *grpc.Server) {
		messagev1.RegisterMessageServiceServer(grpcServer, srv)
	})
	if err != nil {
		panic(err)
	}

	// Graceful shutdown via go-zero proc.
	proc.AddShutdownListener(func() {
		svcCtx.Relay.Stop()
		if svcCtx.Kafka != nil {
			svcCtx.Kafka.Close()
		}
		svcCtx.Relay.WaitCallbacks()
		if deps.DB != nil {
			deps.DB.Close()
		}
	})

	slog.Info("starting message service", "listenOn", cfg.ListenOn)
	zrpcSrv.Start()
}
