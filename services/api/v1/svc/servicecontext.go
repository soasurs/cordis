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

func NewServiceContext(cfg config.Config) *ServiceContext {
	client := zrpc.MustNewClient(cfg.Services.Authenticator)
	return &ServiceContext{
		Cfg:                 cfg,
		AuthenticatorClient: authenticatorv1.NewAuthenticatorServiceClient(client.Conn()),
	}
}
