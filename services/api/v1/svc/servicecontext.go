package svc

import (
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/services/api/v1/config"
	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Cfg                 config.Config
	AuthenticatorClient authenticatorv1.AuthenticatorServiceClient
	UserClient          userv1.UserServiceClient
}

type Dependencies struct {
	AuthenticatorClient authenticatorv1.AuthenticatorServiceClient
	UserClient          userv1.UserServiceClient
}

func NewDependencies(cfg config.Config) (Dependencies, error) {
	authenticatorClient, err := zrpc.NewClient(cfg.Services.Authenticator)
	if err != nil {
		return Dependencies{}, err
	}
	userClient, err := zrpc.NewClient(cfg.Services.User)
	if err != nil {
		return Dependencies{}, err
	}
	return Dependencies{
		AuthenticatorClient: authenticatorv1.NewAuthenticatorServiceClient(authenticatorClient.Conn()),
		UserClient:          userv1.NewUserServiceClient(userClient.Conn()),
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
	if deps.UserClient == nil {
		panic("user client is required")
	}
	return &ServiceContext{
		Cfg:                 cfg,
		AuthenticatorClient: deps.AuthenticatorClient,
		UserClient:          deps.UserClient,
	}
}
