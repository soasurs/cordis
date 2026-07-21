package main

import (
	"context"
	"errors"
	"flag"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"connectrpc.com/connect"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"

	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	"github.com/soasurs/cordis/pkg/probe"
	"github.com/soasurs/cordis/services/api/v1/config"
	"github.com/soasurs/cordis/services/api/v1/observability"
	apiratelimit "github.com/soasurs/cordis/services/api/v1/ratelimit"
	"github.com/soasurs/cordis/services/api/v1/server"
	"github.com/soasurs/cordis/services/api/v1/svc"
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
	probeState := probe.New()
	probeServer, err := probe.StartHTTP(cfg.ProbeServer, probeState)
	if err != nil {
		panic(err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := probeServer.Shutdown(shutdownCtx); err != nil {
			logx.Errorw("shutdown api probe server", logx.Field("error", err))
		}
	}()
	clientIPResolver, err := apiratelimit.NewClientIPResolver(cfg.RateLimit.TrustedProxies)
	if err != nil {
		panic(err)
	}
	interceptors := append(
		observability.ConnectInterceptors(),
		apiratelimit.UnaryInterceptor(svcCtx.RateLimiter, clientIPResolver),
	)
	path, handler := apiv1connect.NewAuthenticatorServiceHandler(
		server.NewAuthenticator(svcCtx),
		connect.WithInterceptors(interceptors...),
	)
	userPath, userHandler := apiv1connect.NewUserServiceHandler(
		server.NewUser(svcCtx),
		connect.WithInterceptors(interceptors...),
	)
	messagePath, messageHandler := apiv1connect.NewMessageServiceHandler(
		server.NewMessage(svcCtx),
		connect.WithInterceptors(interceptors...),
	)
	guildPath, guildHandler := apiv1connect.NewGuildServiceHandler(
		server.NewGuild(svcCtx),
		connect.WithInterceptors(interceptors...),
	)

	mux := http.NewServeMux()
	mux.Handle(path, handler)
	mux.Handle(userPath, userHandler)
	mux.Handle(messagePath, messageHandler)
	mux.Handle(guildPath, guildHandler)

	httpServer := &http.Server{
		Addr:              cfg.ListenOn,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	listener, err := net.Listen("tcp", cfg.ListenOn)
	if err != nil {
		panic(err)
	}
	probeState.SetLiveness(true)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- httpServer.Serve(listener)
	}()

	logx.Infow("api server listening",
		logx.Field("listen_on", cfg.ListenOn),
	)
	probeState.SetReadiness(true)
	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	case <-ctx.Done():
		probeState.SetReadiness(false)
		if err := shutdownHTTPServer(httpServer, serveErr, 5*time.Second); err != nil {
			logx.Errorw("shutdown api server", logx.Field("error", err))
		}
	}
}

func shutdownHTTPServer(server *http.Server, serveErr <-chan error, timeout time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	var closeErr error
	if shutdownErr != nil {
		closeErr = server.Close()
	}
	serveResult := <-serveErr
	if errors.Is(serveResult, http.ErrServerClosed) {
		serveResult = nil
	}
	return errors.Join(shutdownErr, closeErr, serveResult)
}
