package main

import (
	"context"
	"flag"
	"time"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/proc"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	"github.com/soasurs/cordis/pkg/migration"
	"github.com/soasurs/cordis/pkg/probe"
	"github.com/soasurs/cordis/services/media/v1/config"
	migrations "github.com/soasurs/cordis/services/media/v1/db/migrations"
	"github.com/soasurs/cordis/services/media/v1/internal/server"
	"github.com/soasurs/cordis/services/media/v1/internal/svc"
)

var configPath = flag.String("c", "etc/config.yaml", "config file of service")

func main() {
	flag.Parse()
	cfg := new(config.Config)
	conf.LoadConfig(*configPath, cfg, conf.UseEnv())
	cfg.Health = false

	deps, err := svc.NewDependencies(*cfg)
	if err != nil {
		panic(err)
	}

	svcCtx := svc.NewServiceContextWithDependencies(*cfg, deps)

	ctx := context.Background()
	if err := migration.ApplyNamed(ctx, deps.DB, "media", migrations.FS); err != nil {
		panic(err)
	}

	probeState := probe.New()
	probeState.SetLiveness(true)
	proc.AddWrapUpListener(func() { probeState.SetReadiness(false) })

	srv := server.New(svcCtx)
	s, err := zrpc.NewServer(cfg.RpcServerConf, func(grpcServer *grpc.Server) {
		mediav1.RegisterMediaServiceServer(grpcServer, srv.GRPC())
		probeState.RegisterGRPC(grpcServer)
	})
	if err != nil {
		panic(err)
	}

	cleanupInterval := time.Duration(svcCtx.Cfg.Media.StagingCleanupInterval()) * time.Second
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	proc.AddWrapUpListener(func() {
		cleanupCancel()
		if err := srv.CleanupExpired(context.Background()); err != nil {
			logx.Errorw("final staging cleanup failed", logx.Field("err", err))
		}
	})
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-cleanupCtx.Done():
				return
			case <-ticker.C:
				if err := srv.CleanupExpired(context.Background()); err != nil {
					logx.Errorw("staging cleanup failed", logx.Field("err", err))
				}
			}
		}
	}()

	probeState.SetReadiness(true)
	s.Start()
}
