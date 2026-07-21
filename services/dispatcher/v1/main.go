package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/zrpc"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/observability"
	"github.com/soasurs/cordis/pkg/probe"
	"github.com/soasurs/cordis/pkg/sessionregistry"
	"github.com/soasurs/cordis/services/dispatcher/v1/config"
	"github.com/soasurs/cordis/services/dispatcher/v1/internal/discovery"
	"github.com/soasurs/cordis/services/dispatcher/v1/internal/server"
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

	rds, err := redis.NewRedis(cfg.Redis)
	if err != nil {
		panic(err)
	}
	registry, err := sessionregistry.New(cfg.SessionRegistry)
	if err != nil {
		panic(err)
	}
	defer registry.Close()
	userRPCClient, err := zrpc.NewClient(cfg.Services.User)
	if err != nil {
		panic(err)
	}
	defer userRPCClient.Conn().Close()
	dispatcher := server.New(*cfg, discovery.NewRedisResolver(rds, registry), userv1.NewUserServiceClient(userRPCClient.Conn()))
	probeState := probe.New()
	probeServer, err := probe.StartHTTP(cfg.ProbeServer, probeState)
	if err != nil {
		panic(err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := probeServer.Shutdown(shutdownCtx); err != nil {
			logx.Errorw("shutdown dispatcher probe server", logx.Field("error", err))
		}
	}()
	probeState.SetLiveness(true)

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	go func() {
		<-signalCtx.Done()
		probeState.SetReadiness(false)
		cancelRun()
	}()
	probeState.SetReadiness(true)
	dispatcher.Run(runCtx)
}
