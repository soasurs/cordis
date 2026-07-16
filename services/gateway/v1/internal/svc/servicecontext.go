package svc

import (
	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/soasurs/cordis/services/gateway/v1/config"
	"github.com/soasurs/cordis/services/gateway/v1/internal/discovery"
)

type ServiceContext struct {
	Cfg      config.Config
	Resolver discovery.Resolver
}

type Dependencies struct {
	Resolver discovery.Resolver
}

func NewDependencies(cfg config.Config) (Dependencies, error) {
	rds, err := redis.NewRedis(cfg.Redis)
	if err != nil {
		return Dependencies{}, err
	}
	return Dependencies{
		Resolver: discovery.NewRedisResolver(rds),
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
	if deps.Resolver == nil {
		panic("session resolver is required")
	}
	return &ServiceContext{
		Cfg: cfg, Resolver: deps.Resolver,
	}
}
