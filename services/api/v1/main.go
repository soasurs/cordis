package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	"github.com/soasurs/cordis/services/api/v1/config"
	"github.com/soasurs/cordis/services/api/v1/server"
	"github.com/soasurs/cordis/services/api/v1/svc"
	"github.com/zeromicro/go-zero/core/conf"
)

var configPath = flag.String("c", "etc/config.yaml", "config file of service")

func main() {
	flag.Parse()

	cfg := new(config.Config)
	if err := conf.LoadConfig(*configPath, cfg); err != nil {
		panic(err)
	}

	svcCtx := svc.NewServiceContext(*cfg)
	path, handler := apiv1connect.NewAuthenticatorServiceHandler(server.NewAuthenticator(svcCtx))

	mux := http.NewServeMux()
	mux.Handle(path, handler)

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
			log.Printf("shutdown api server: %v", err)
		}
	}()

	log.Printf("%s listening on %s", cfg.Name, cfg.ListenOn)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		panic(err)
	}
}
