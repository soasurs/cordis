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

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/soasurs/cordis/services/gateway/v1/config"
	"github.com/soasurs/cordis/services/gateway/v1/internal/server"
	"github.com/soasurs/cordis/services/gateway/v1/internal/svc"
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

	svcCtx := svc.NewServiceContext(*cfg)
	gateway := server.New(svcCtx)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpServer := &http.Server{
		Addr:              cfg.ListenOn,
		Handler:           gateway.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logx.Errorw("shutdown gateway server",
				logx.Field("error", err),
			)
		}
	}()

	logx.Infow("gateway server listening",
		logx.Field("listen_on", cfg.ListenOn),
		logx.Field("websocket_path", cfg.Gateway.WebSocketRoute()),
	)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		panic(err)
	}
}
