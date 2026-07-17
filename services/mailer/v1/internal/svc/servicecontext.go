package svc

import (
	"errors"

	"github.com/soasurs/cordis/services/mailer/v1/config"
	"github.com/soasurs/cordis/services/mailer/v1/internal/provider"
)

type ServiceContext struct {
	Cfg      config.Config
	Provider provider.Provider
}

type Dependencies struct {
	Provider provider.Provider
}

func NewDependencies(cfg config.Config) (Dependencies, error) {
	switch cfg.Mailer.Provider {
	case "", "noop":
		return Dependencies{Provider: provider.NewNoop()}, nil
	default:
		return Dependencies{}, errors.New("unsupported mailer provider")
	}
}

func NewServiceContextWithDependencies(cfg config.Config, deps Dependencies) *ServiceContext {
	if deps.Provider == nil {
		panic("mailer provider is required")
	}
	return &ServiceContext{
		Cfg:      cfg,
		Provider: deps.Provider,
	}
}
