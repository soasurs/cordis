package server

import (
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/svc"
)

type guildServer struct {
	svcCtx *svc.ServiceContext
}

func New(svcCtx *svc.ServiceContext) guildv1.GuildServiceServer {
	return &guildServer{svcCtx: svcCtx}
}
