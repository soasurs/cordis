package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"connectrpc.com/connect"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	"github.com/soasurs/cordis/services/api/v1/config"
	"github.com/soasurs/cordis/services/api/v1/observability"
	"github.com/soasurs/cordis/services/api/v1/server"
	"github.com/soasurs/cordis/services/api/v1/svc"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
)

var configPath = flag.String("c", "etc/config.yaml", "config file of service")

func main() {
	flag.Parse()

	cfg := new(config.Config)
	if err := conf.LoadConfig(*configPath, cfg, conf.UseEnv()); err != nil {
		panic(err)
	}
	cfg.Log.ServiceName = cfg.Name
	logx.MustSetup(cfg.Log)
	defer logx.Close()

	obsRuntime, err := observability.SetUp(context.Background(), cfg.Name, cfg.Observability)
	if err != nil {
		panic(err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := obsRuntime.Shutdown(shutdownCtx); err != nil {
			logx.Errorw("shutdown api observability",
				logx.Field("error", err),
			)
		}
	}()

	svcCtx := svc.NewServiceContext(*cfg)
	path, handler := apiv1connect.NewAuthenticatorServiceHandler(
		server.NewAuthenticator(svcCtx),
		connect.WithInterceptors(observability.ConnectInterceptors()...),
	)
	userPath, userHandler := apiv1connect.NewUserServiceHandler(
		server.NewUser(svcCtx),
		connect.WithInterceptors(observability.ConnectInterceptors()...),
	)

	mux := http.NewServeMux()
	mux.Handle(path, handler)
	mux.Handle(userPath, userHandler)

	httpServer := &http.Server{
		Addr:              cfg.ListenOn,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logx.Errorw("shutdown api server",
				logx.Field("error", err),
			)
		}
	}()

	logx.Infow("api server listening",
		logx.Field("listen_on", cfg.ListenOn),
	)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		panic(err)
	}
}
