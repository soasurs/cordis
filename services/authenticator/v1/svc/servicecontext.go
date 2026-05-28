package svc

import "github.com/soasurs/cordis/services/authenticator/v1/config"

type ServiceContext struct {
}

func NewServiceContext(cfg config.Config) *ServiceContext {
	return &ServiceContext{}
}
