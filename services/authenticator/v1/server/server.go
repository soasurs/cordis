package server

import (
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/services/authenticator/v1/svc"
)

type authenticatorServer struct {
	svcCtx *svc.ServiceContext
	authenticatorv1.UnimplementedAuthenticatorServiceServer
}

func New(svcCtx *svc.ServiceContext) authenticatorv1.AuthenticatorServiceServer {
	return &authenticatorServer{
		svcCtx: svcCtx,
	}
}
