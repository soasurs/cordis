package server

import (
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/services/message/v1/internal/svc"
)

type messageServer struct {
	svcCtx *svc.ServiceContext
}

func New(svcCtx *svc.ServiceContext) messagev1.MessageServiceServer {
	return &messageServer{svcCtx: svcCtx}
}
