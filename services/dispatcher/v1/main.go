package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"

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

	rds, err := redis.NewRedis(cfg.Redis)
	if err != nil {
		panic(err)
	}
	registry, err := sessionregistry.New(cfg.SessionRegistry)
	if err != nil {
		panic(err)
	}
	defer registry.Close()
	dispatcher := server.New(*cfg, discovery.NewRedisResolver(rds, registry))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dispatcher.Run(ctx)
}
