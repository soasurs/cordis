package probe

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestHTTPHandlers(t *testing.T) {
	state := New()
	handler := NewHTTPHandler(state, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "metrics")
	}))

	require.Equal(t, http.StatusServiceUnavailable, responseCode(t, handler, http.MethodGet, "/livez"))
	require.Equal(t, http.StatusServiceUnavailable, responseCode(t, handler, http.MethodHead, "/readyz"))
	require.Equal(t, http.StatusMethodNotAllowed, responseCode(t, handler, http.MethodPost, "/livez"))
	require.Equal(t, http.StatusNotFound, responseCode(t, handler, http.MethodGet, "/health"))
	require.Equal(t, http.StatusNotFound, responseCode(t, handler, http.MethodGet, "/healthz"))
	require.Equal(t, http.StatusOK, responseCode(t, handler, http.MethodGet, "/metrics"))

	state.SetLiveness(true)
	require.Equal(t, http.StatusNoContent, responseCode(t, handler, http.MethodGet, "/livez"))
	require.Equal(t, http.StatusServiceUnavailable, responseCode(t, handler, http.MethodGet, "/readyz"))

	state.SetReadiness(true)
	require.Equal(t, http.StatusNoContent, responseCode(t, handler, http.MethodGet, "/readyz"))
	state.SetReadiness(false)
	require.Equal(t, http.StatusServiceUnavailable, responseCode(t, handler, http.MethodGet, "/readyz"))
}

func TestGRPCCheckAndWatch(t *testing.T) {
	state := New()
	client, cleanup := newHealthClient(t, state)
	defer cleanup()

	checkStatus(t, client, LivenessService, healthv1.HealthCheckResponse_NOT_SERVING)
	checkStatus(t, client, ReadinessService, healthv1.HealthCheckResponse_NOT_SERVING)
	checkStatus(t, client, "", healthv1.HealthCheckResponse_NOT_SERVING)
	_, err := client.Check(t.Context(), &healthv1.HealthCheckRequest{Service: "unknown"})
	require.Equal(t, codes.NotFound, status.Code(err))

	watch, err := client.Watch(t.Context(), &healthv1.HealthCheckRequest{Service: ReadinessService})
	require.NoError(t, err)
	response, err := watch.Recv()
	require.NoError(t, err)
	require.Equal(t, healthv1.HealthCheckResponse_NOT_SERVING, response.GetStatus())

	state.SetReadiness(true)
	response, err = watch.Recv()
	require.NoError(t, err)
	require.Equal(t, healthv1.HealthCheckResponse_SERVING, response.GetStatus())
	checkStatus(t, client, "", healthv1.HealthCheckResponse_SERVING)

	unknown, err := client.Watch(t.Context(), &healthv1.HealthCheckRequest{Service: "unknown"})
	require.NoError(t, err)
	response, err = unknown.Recv()
	require.NoError(t, err)
	require.Equal(t, healthv1.HealthCheckResponse_SERVICE_UNKNOWN, response.GetStatus())
}

func TestConcurrentTransitions(t *testing.T) {
	state := New()
	var group sync.WaitGroup
	for range 100 {
		group.Go(func() {
			state.SetLiveness(true)
			state.SetReadiness(true)
			state.SetReadiness(false)
		})
	}
	group.Wait()
	state.SetLiveness(true)
	state.SetReadiness(false)

	liveness, err := state.Check(t.Context(), &healthv1.HealthCheckRequest{Service: LivenessService})
	require.NoError(t, err)
	require.Equal(t, healthv1.HealthCheckResponse_SERVING, liveness.GetStatus())
	readiness, err := state.Check(t.Context(), &healthv1.HealthCheckRequest{Service: ReadinessService})
	require.NoError(t, err)
	require.Equal(t, healthv1.HealthCheckResponse_NOT_SERVING, readiness.GetStatus())
}

func TestHTTPServerShutdownClearsOnlyReadiness(t *testing.T) {
	state := New()
	state.SetLiveness(true)
	state.SetReadiness(true)
	server, err := StartHTTP(HTTPConfig{ListenOn: "127.0.0.1:0"}, state)
	require.NoError(t, err)

	shutdownCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, server.Shutdown(shutdownCtx))

	liveness, err := state.Check(t.Context(), &healthv1.HealthCheckRequest{Service: LivenessService})
	require.NoError(t, err)
	require.Equal(t, healthv1.HealthCheckResponse_SERVING, liveness.GetStatus())
	readiness, err := state.Check(t.Context(), &healthv1.HealthCheckRequest{Service: ReadinessService})
	require.NoError(t, err)
	require.Equal(t, healthv1.HealthCheckResponse_NOT_SERVING, readiness.GetStatus())
}

func responseCode(t *testing.T, handler http.Handler, method, path string) int {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code
}

func newHealthClient(t *testing.T, state *State) (healthv1.HealthClient, func()) {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	state.RegisterGRPC(server)
	go func() {
		_ = server.Serve(listener)
	}()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	return healthv1.NewHealthClient(conn), func() {
		require.NoError(t, conn.Close())
		server.Stop()
		require.NoError(t, listener.Close())
	}
}

func checkStatus(t *testing.T, client healthv1.HealthClient, service string, expected healthv1.HealthCheckResponse_ServingStatus) {
	t.Helper()
	response, err := client.Check(t.Context(), &healthv1.HealthCheckRequest{Service: service})
	require.NoError(t, err)
	require.Equal(t, expected, response.GetStatus())
}
