package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/zeromicro/go-zero/core/logx"
)

// HTTPConfig configures the internal HTTP listener for probes and metrics.
type HTTPConfig struct {
	ListenOn string
}

// HTTPServer is the internal HTTP listener for probes and Prometheus metrics.
type HTTPServer struct {
	server *http.Server
	state  *State
}

// StartHTTP starts an internal listener exposing /livez, /readyz, and /metrics.
func StartHTTP(cfg HTTPConfig, state *State) (*HTTPServer, error) {
	if cfg.ListenOn == "" {
		return nil, errors.New("probe listen address is required")
	}
	if state == nil {
		return nil, errors.New("probe state is required")
	}

	server := &http.Server{
		Addr:              cfg.ListenOn,
		Handler:           NewHTTPHandler(state, promhttp.Handler()),
		ReadHeaderTimeout: 5 * time.Second,
	}
	listener, err := net.Listen("tcp", cfg.ListenOn)
	if err != nil {
		return nil, fmt.Errorf("listen probe server %s: %w", cfg.ListenOn, err)
	}

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logx.Errorw("serve probe server",
				logx.Field("listen_on", cfg.ListenOn),
				logx.Field("error", err),
			)
		}
	}()
	return &HTTPServer{server: server, state: state}, nil
}

// NewHTTPHandler returns the fixed internal operational endpoint set.
func NewHTTPHandler(state *State, metrics http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/livez", state.LivenessHandler())
	mux.Handle("/readyz", state.ReadinessHandler())
	if metrics != nil {
		mux.Handle("/metrics", metrics)
	}
	return mux
}

// Shutdown clears readiness before gracefully stopping the internal listener.
// Liveness remains serving in the shared state until the process exits.
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.state.SetReadiness(false)
	return s.server.Shutdown(ctx)
}
