package server

import (
	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	"github.com/soasurs/cordis/services/media/v1/internal/svc"
)

type MediaServer struct {
	svcCtx *svc.ServiceContext
}

func New(svcCtx *svc.ServiceContext) *MediaServer {
	return &MediaServer{svcCtx: svcCtx}
}

// GRPC returns the gRPC service server implementation for registration.
func (s *MediaServer) GRPC() mediav1.MediaServiceServer {
	return s
}
