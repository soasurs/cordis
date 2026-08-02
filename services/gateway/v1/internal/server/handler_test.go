package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/pkg/clientip"
	"github.com/soasurs/cordis/services/gateway/v1/config"
	"github.com/soasurs/cordis/services/gateway/v1/internal/svc"
)

func TestPublicHandlerDoesNotExposeOperationalEndpoints(t *testing.T) {
	gateway := &Server{svcCtx: &svc.ServiceContext{Cfg: config.Config{
		Gateway: config.GatewayConfig{WebSocketPath: "/ws"},
	}}}
	for _, path := range []string{"/health", "/healthz", "/livez", "/readyz", "/metrics"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		gateway.Handler().ServeHTTP(response, request)
		require.Equal(t, http.StatusNotFound, response.Code, path)
	}
}

func TestRootWebSocketRouteDoesNotCatchOtherPaths(t *testing.T) {
	gateway := &Server{svcCtx: &svc.ServiceContext{Cfg: config.Config{
		Gateway: config.GatewayConfig{WebSocketPath: "/"},
	}}}
	for _, path := range []string{"/health", "/livez", "/metrics", "/ws"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		gateway.Handler().ServeHTTP(response, request)
		require.Equal(t, http.StatusNotFound, response.Code, path)
	}
}

func TestWebSocketOriginPatterns(t *testing.T) {
	gateway := New(svc.NewServiceContextWithDependencies(config.Config{
		Gateway: config.GatewayConfig{
			WebSocketPath:  "/",
			OriginPatterns: []string{"https://app.example.com"},
		},
	}, svc.Dependencies{Resolver: fakeResolver{}}))
	httpServer := httptest.NewServer(gateway.Handler())
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/"
	allowedHeader := make(http.Header)
	allowedHeader.Set("Origin", "https://app.example.com")
	conn, _, err := websocket.Dial(t.Context(), wsURL, &websocket.DialOptions{HTTPHeader: allowedHeader})
	require.NoError(t, err)
	conn.CloseNow()

	deniedHeader := make(http.Header)
	deniedHeader.Set("Origin", "https://other.example.com")
	conn, response, err := websocket.Dial(t.Context(), wsURL, &websocket.DialOptions{HTTPHeader: deniedHeader})
	require.Error(t, err)
	require.Nil(t, conn)
	require.NotNil(t, response)
	require.Equal(t, http.StatusForbidden, response.StatusCode)
}

func TestDrainClosesActiveWebSocketsAndRejectsUpgrades(t *testing.T) {
	clientIPResolver, err := clientip.New(nil)
	require.NoError(t, err)
	gateway := &Server{
		svcCtx: &svc.ServiceContext{
			Cfg: config.Config{Gateway: config.GatewayConfig{
				WebSocketPath:          "/ws",
				IdentifyTimeoutSeconds: 30,
			}},
			ClientIPResolver: clientIPResolver,
		},
		gatewayID:   "gateway-test",
		connections: make(map[*client]struct{}),
		drainDone:   make(chan struct{}),
	}
	httpServer := httptest.NewServer(gateway.Handler())
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"
	conn, _, err := websocket.Dial(t.Context(), wsURL, nil)
	require.NoError(t, err)
	defer conn.CloseNow()
	var hello envelope
	require.NoError(t, wsjson.Read(t.Context(), conn, &hello))

	drainCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	drainErr := make(chan error, 1)
	go func() {
		drainErr <- gateway.Drain(drainCtx)
	}()
	var message envelope
	err = wsjson.Read(t.Context(), conn, &message)
	require.Equal(t, websocket.StatusServiceRestart, websocket.CloseStatus(err))
	require.NoError(t, <-drainErr)

	response, err := http.Get(httpServer.URL + "/ws")
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
}
