package main

import (
	"flag"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/proc"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"

	mailerv1 "github.com/soasurs/cordis/gen/mailer/v1"
	"github.com/soasurs/cordis/pkg/probe"
	"github.com/soasurs/cordis/services/mailer/v1/config"
	"github.com/soasurs/cordis/services/mailer/v1/internal/server"
	"github.com/soasurs/cordis/services/mailer/v1/internal/svc"
)

var configPath = flag.String("c", "etc/config.yaml", "config file of service")

func main() {
	flag.Parse()

	cfg := new(config.Config)
	if err := conf.LoadConfig(*configPath, cfg, conf.UseEnv()); err != nil {
		panic(err)
	}
	cfg.Health = false

	deps, err := svc.NewDependencies(*cfg)
	if err != nil {
		panic(err)
	}
	svcCtx := svc.NewServiceContextWithDependencies(*cfg, deps)
	probeState := probe.New()
	probeState.SetLiveness(true)
	proc.AddWrapUpListener(func() {
		probeState.SetReadiness(false)
	})

	server := server.New(svcCtx)
	s, err := zrpc.NewServer(cfg.RpcServerConf, func(grpcServer *grpc.Server) {
		mailerv1.RegisterMailerServiceServer(grpcServer, server)
		probeState.RegisterGRPC(grpcServer)
	})
	if err != nil {
		panic(err)
	}

	probeState.SetReadiness(true)
	s.Start()
}
