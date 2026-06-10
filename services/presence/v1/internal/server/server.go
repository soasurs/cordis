package server

import (
	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	"github.com/soasurs/cordis/services/presence/v1/internal/svc"
)

type presenceServer struct {
	svcCtx *svc.ServiceContext
}

func New(svcCtx *svc.ServiceContext) presencev1.PresenceServiceServer {
	return &presenceServer{svcCtx: svcCtx}
}
