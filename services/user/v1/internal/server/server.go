package server

import (
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/services/user/v1/internal/svc"
)

type userServer struct {
	svcCtx *svc.ServiceContext
}

func New(svcCtx *svc.ServiceContext) userv1.UserServiceServer {
	return &userServer{svcCtx: svcCtx}
}
