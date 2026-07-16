package svc

import (
	"github.com/zeromicro/go-zero/zrpc"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	"github.com/soasurs/cordis/services/gateway/v1/config"
)

type ServiceContext struct {
	Cfg                 config.Config
	AuthenticatorClient authenticatorv1.AuthenticatorServiceClient
	PresenceClient      presencev1.PresenceServiceClient
}

type Dependencies struct {
	AuthenticatorClient authenticatorv1.AuthenticatorServiceClient
	PresenceClient      presencev1.PresenceServiceClient
}

func NewDependencies(cfg config.Config) (Dependencies, error) {
	authClient, err := zrpc.NewClient(cfg.Services.Authenticator)
	if err != nil {
		return Dependencies{}, err
	}
	presenceClient, err := zrpc.NewClient(cfg.Services.Presence)
	if err != nil {
		return Dependencies{}, err
	}
	return Dependencies{
		AuthenticatorClient: authenticatorv1.NewAuthenticatorServiceClient(authClient.Conn()),
		PresenceClient:      presencev1.NewPresenceServiceClient(presenceClient.Conn()),
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
	if deps.PresenceClient == nil {
		panic("presence client is required")
	}
	return &ServiceContext{
		Cfg:                 cfg,
		AuthenticatorClient: deps.AuthenticatorClient,
		PresenceClient:      deps.PresenceClient,
	}
}
