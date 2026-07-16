package main

import (
	"flag"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/proc"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/services/authenticator/v1/config"
	"github.com/soasurs/cordis/services/authenticator/v1/server"
	"github.com/soasurs/cordis/services/authenticator/v1/svc"
)

var configPath = flag.String("c", "etc/config.yaml", "config file of service")

func main() {
	flag.Parse()

	cfg := new(config.Config)
	if err := conf.LoadConfig(*configPath, cfg, conf.UseEnv()); err != nil {
		panic(err)
	}

	deps, err := svc.NewDependencies(*cfg)
	if err != nil {
		panic(err)
	}
	svcCtx := svc.NewServiceContextWithDependencies(*cfg, deps)

	proc.AddShutdownListener(func() {
		if deps.DB != nil {
			deps.DB.Close()
		}
	})

	server := server.New(svcCtx)
	s, err := zrpc.NewServer(cfg.RpcServerConf, func(grpcServer *grpc.Server) {
		authenticatorv1.RegisterAuthenticatorServiceServer(grpcServer, server)
	})
	if err != nil {
		panic(err)
	}

	s.Start()
}
