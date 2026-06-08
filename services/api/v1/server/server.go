package server

import (
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

type authenticatorServer struct {
	svcCtx *svc.ServiceContext
}

func NewAuthenticator(svcCtx *svc.ServiceContext) apiv1connect.AuthenticatorServiceHandler {
	return &authenticatorServer{svcCtx: svcCtx}
}
