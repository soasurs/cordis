package svc

import (
	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/soasurs/cordis/services/presence/v1/config"
	"github.com/soasurs/cordis/services/presence/v1/internal/store"
)

type ServiceContext struct {
	Cfg   config.Config
	Store store.Store
}

type Dependencies struct {
	Store store.Store
	Redis *redis.Redis
}

func NewDependencies(cfg config.Config) (Dependencies, error) {
	rds, err := redis.NewRedis(cfg.Redis)
	if err != nil {
		return Dependencies{}, err
	}

	return Dependencies{
		Store: store.NewRedisStore(rds, cfg.Presence.GatewayTTL(), cfg.Presence.RouteTTL(), cfg.Presence.UserSessionTTL()),
		Redis: rds,
	}, nil
}

func NewServiceContext(cfg config.Config) *ServiceContext {
	deps, err := NewDependencies(cfg)
	if err != nil {
		panic(err)
	}
	return NewServiceContextWithDependencies(cfg, deps)
}

func NewServiceContextWithDependencies(cfg config.Config, deps Dependencies) *ServiceContext {
	if deps.Store == nil {
		panic("presence store is required")
	}
	return &ServiceContext{
		Cfg:   cfg,
		Store: deps.Store,
	}
}
