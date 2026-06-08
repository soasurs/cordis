package svc

import (
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/services/api/v1/config"
	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Cfg                 config.Config
	AuthenticatorClient authenticatorv1.AuthenticatorServiceClient
}

type Dependencies struct {
	AuthenticatorClient authenticatorv1.AuthenticatorServiceClient
}

func NewDependencies(cfg config.Config) (Dependencies, error) {
	client, err := zrpc.NewClient(cfg.Services.Authenticator)
	if err != nil {
		return Dependencies{}, err
	}
	return Dependencies{
		AuthenticatorClient: authenticatorv1.NewAuthenticatorServiceClient(client.Conn()),
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
	if deps.AuthenticatorClient == nil {
		panic("authenticator client is required")
	}
	return &ServiceContext{
		Cfg:                 cfg,
		AuthenticatorClient: deps.AuthenticatorClient,
	}
}
