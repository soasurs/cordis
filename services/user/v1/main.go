package main

import (
	"flag"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/services/user/v1/config"
	"github.com/soasurs/cordis/services/user/v1/internal/server"
	"github.com/soasurs/cordis/services/user/v1/internal/svc"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

var configPath = flag.String("c", "etc/config.yaml", "config file of service")

func main() {
	cfg := new(config.Config)
	if err := conf.LoadConfig(*configPath, cfg); err != nil {
		panic(err)
	}

	svc := svc.NewServiceContext(*cfg)
	server := server.New(svc)
	s, err := zrpc.NewServer(cfg.RpcServerConf, func(grpcServer *grpc.Server) {
		userv1.RegisterUserServiceServer(grpcServer, server)
	})
	if err != nil {
		panic(err)
	}

	s.Start()
}
