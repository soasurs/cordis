package svc

import (
	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/soasurs/cordis/pkg/sessionregistry"
	"github.com/soasurs/cordis/services/gateway/v1/config"
	"github.com/soasurs/cordis/services/gateway/v1/internal/discovery"
)

type ServiceContext struct {
	Cfg      config.Config
	Resolver discovery.Resolver
	Registry sessionregistry.Directory
}

type Dependencies struct {
	Resolver discovery.Resolver
	Registry sessionregistry.Directory
}

func NewDependencies(cfg config.Config) (Dependencies, error) {
	rds, err := redis.NewRedis(cfg.Redis)
	if err != nil {
		return Dependencies{}, err
	}
	registry, err := sessionregistry.New(cfg.SessionRegistry)
	if err != nil {
		return Dependencies{}, err
	}
	return Dependencies{
		Resolver: discovery.New(rds, registry),
		Registry: registry,
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
		Cfg: cfg, Resolver: deps.Resolver, Registry: deps.Registry,
	}
}

func (s *ServiceContext) Close() error {
	if s.Registry != nil {
		return s.Registry.Close()
	}
	return nil
}
