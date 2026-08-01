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

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/pkg/probe"
	"github.com/soasurs/cordis/services/guild/v1/config"
	"github.com/soasurs/cordis/services/guild/v1/internal/server"
	"github.com/soasurs/cordis/services/guild/v1/internal/svc"
)

var configPath = flag.String("c", "etc/config.yaml", "config file of service")

func main() {
	flag.Parse()

	cfg := new(config.Config)
	if err := conf.LoadConfig(*configPath, cfg, conf.UseEnv()); err != nil {
		panic(err)
	}
	shutdownTimeout := cfg.ShutdownDuration()
	// go-zero starts shutdown listeners after its one-second wrap-up phase.
	proc.SetTimeToForceQuit(shutdownTimeout + time.Second)
	cfg.Health = false
	cfg.Log.ServiceName = cfg.Name
	logx.MustSetup(cfg.Log)

	deps, err := svc.NewDependencies(*cfg)
	if err != nil {
		panic(err)
	}
	if deps.DB != nil {
		defer deps.DB.Close()
	}
	if deps.Kafka != nil {
		defer deps.Kafka.Close()
	}
	svcCtx := svc.NewServiceContextWithDependencies(*cfg, deps)
	srv := server.New(svcCtx)
	projector, err := server.NewProfileProjector(svcCtx)
	if err != nil {
		panic(err)
	}
	projectorCtx, cancelProjector := context.WithCancel(context.Background())
	projectorDone := make(chan struct{})
	proc.AddShutdownListener(func() {
		cancelProjector()
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancelShutdown()
		if err := projector.CloseContext(shutdownCtx); err != nil && shutdownCtx.Err() == nil {
			logx.Errorw("close profile projector", logx.Field("error", err))
		}
		select {
		case <-projectorDone:
		case <-shutdownCtx.Done():
			logx.Error("profile projector did not stop before shutdown timeout")
		}
	})
	go func() {
		defer close(projectorDone)
		projector.Run(projectorCtx)
	}()
	probeState := probe.New()
	probeState.SetLiveness(true)
	proc.AddWrapUpListener(func() {
		probeState.SetReadiness(false)
	})

	zrpcSrv, err := zrpc.NewServer(cfg.RpcServerConf, func(grpcServer *grpc.Server) {
		guildv1.RegisterGuildServiceServer(grpcServer, srv)
		probeState.RegisterGRPC(grpcServer)
	})
	if err != nil {
		panic(err)
	}

	logx.Infow("starting guild service", logx.Field("listen_on", cfg.ListenOn))
	probeState.SetReadiness(true)
	zrpcSrv.Start()
}
