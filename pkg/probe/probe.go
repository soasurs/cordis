// Package probe provides shared liveness and readiness state for HTTP and gRPC probes.
package probe

import (
	"context"
	"net/http"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
)

const (
	// LivenessService is the gRPC health service name used by liveness probes.
	LivenessService = "liveness"
	// ReadinessService is the gRPC health service name used by readiness probes.
	ReadinessService = "readiness"
)

// State owns independent liveness and readiness state shared by HTTP and gRPC.
// The empty gRPC service name reports overall readiness.
type State struct {
	health *health.Server
}

// New returns probe state with both liveness and readiness initially not serving.
func New() *State {
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthv1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus(LivenessService, healthv1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus(ReadinessService, healthv1.HealthCheckResponse_NOT_SERVING)
	return &State{health: healthServer}
}

// SetLiveness changes the liveness state.
func (s *State) SetLiveness(serving bool) {
	s.health.SetServingStatus(LivenessService, servingStatus(serving))
}

// SetReadiness changes both readiness and the empty-name overall health state.
func (s *State) SetReadiness(serving bool) {
	status := servingStatus(serving)
	s.health.SetServingStatus(ReadinessService, status)
	s.health.SetServingStatus("", status)
}

// RegisterGRPC registers this state as the standard gRPC health service.
func (s *State) RegisterGRPC(registrar grpc.ServiceRegistrar) {
	healthv1.RegisterHealthServer(registrar, s)
}

// Check implements grpc.health.v1.Health.
func (s *State) Check(ctx context.Context, request *healthv1.HealthCheckRequest) (*healthv1.HealthCheckResponse, error) {
	return s.health.Check(ctx, request)
}

// List implements grpc.health.v1.Health.
func (s *State) List(ctx context.Context, request *healthv1.HealthListRequest) (*healthv1.HealthListResponse, error) {
	return s.health.List(ctx, request)
}

// Watch implements grpc.health.v1.Health.
func (s *State) Watch(request *healthv1.HealthCheckRequest, stream healthv1.Health_WatchServer) error {
	return s.health.Watch(request, stream)
}

// LivenessHandler returns an HTTP handler backed by the liveness state.
func (s *State) LivenessHandler() http.Handler {
	return s.httpHandler(LivenessService)
}

// ReadinessHandler returns an HTTP handler backed by the readiness state.
func (s *State) ReadinessHandler() http.Handler {
	return s.httpHandler(ReadinessService)
}

func (s *State) httpHandler(service string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		response, err := s.Check(r.Context(), &healthv1.HealthCheckRequest{Service: service})
		if err != nil || response.GetStatus() != healthv1.HealthCheckResponse_SERVING {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func servingStatus(serving bool) healthv1.HealthCheckResponse_ServingStatus {
	if serving {
		return healthv1.HealthCheckResponse_SERVING
	}
	return healthv1.HealthCheckResponse_NOT_SERVING
}
