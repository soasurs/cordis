package server

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/observability"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/session/v1/internal/store"
)

func TestRefreshSessionLeasesCreatesBoundedParentSpan(t *testing.T) {
	tests := []struct {
		name             string
		ownerErr         error
		presenceErr      error
		wantResult       string
		wantOwnerType    string
		wantPresenceType string
		wantStatus       string
	}{
		{
			name:             "success",
			wantResult:       "success",
			wantOwnerType:    "none",
			wantPresenceType: "none",
			wantStatus:       "Unset",
		},
		{
			name:             "redacted failures",
			ownerErr:         status.Error(codes.Unavailable, "sensitive-owner-key"),
			presenceErr:      status.Error(codes.DeadlineExceeded, "sensitive-rpc-address"),
			wantResult:       "partial_failure",
			wantOwnerType:    "unavailable",
			wantPresenceType: "timeout",
			wantStatus:       "Error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSampler(sdktrace.AlwaysSample()),
				sdktrace.WithSyncer(exporter),
			)
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			tracer := provider.Tracer("lease-test")
			leaseStore := &tracedLeaseStore{fakeStore: new(fakeStore), tracer: tracer, err: tt.ownerErr}
			presence := &tracedLeasePresence{tracer: tracer, err: tt.presenceErr}
			server := newTestServer()
			server.tracer = provider.Tracer(observability.SessionInstrumentationName)
			server.svcCtx.Store = leaseStore
			server.svcCtx.PresenceClient = presence
			session := &logicalSession{
				id:                "sensitive-session-id",
				userID:            1001,
				gatewayID:         "sensitive-gateway-id",
				gatewayGeneration: "sensitive-generation",
				deviceType:        "sensitive-device",
				guilds:            make(map[int64]struct{}),
			}
			server.sessions[session.id] = session

			server.refreshSessionLeasesWithSpread(t.Context(), 0)

			spans := exporter.GetSpans()
			require.Len(t, spans, 3)
			cycle := leaseSpanByName(t, spans, "session.lease.refresh")
			redisSpan := leaseSpanByName(t, spans, "redis")
			presenceSpan := leaseSpanByName(t, spans, "presence.refresh")
			for _, child := range []tracetest.SpanStub{redisSpan, presenceSpan} {
				require.Equal(t, cycle.SpanContext.TraceID(), child.SpanContext.TraceID())
				require.Equal(t, cycle.SpanContext.SpanID(), child.Parent.SpanID())
			}
			attrs := leaseSpanAttributes(cycle)
			require.Equal(t, int64(1), attrs["cordis.session.lease.session_count"])
			require.Equal(t, int64(1), attrs["cordis.session.lease.batch_count"])
			require.Equal(t, int64(500), attrs["cordis.session.lease.batch_size"])
			require.Equal(t, int64(1), attrs["cordis.session.lease.completed_batch_count"])
			require.Equal(t, int64(30_000), attrs["cordis.session.lease.interval_ms"])
			require.Equal(t, int64(0), attrs["cordis.session.lease.spread_ms"])
			require.Equal(t, tt.wantResult, attrs["cordis.session.lease.result"])
			require.Equal(t, tt.wantOwnerType, attrs["cordis.session.lease.owner_error_type"])
			require.Equal(t, tt.wantPresenceType, attrs["cordis.session.lease.presence_error_type"])
			require.Equal(t, tt.wantStatus, cycle.Status.Code.String())
			if tt.ownerErr == nil {
				require.Equal(t, int64(0), attrs["cordis.session.lease.owner_failure_count"])
			} else {
				require.Equal(t, int64(1), attrs["cordis.session.lease.owner_failure_count"])
			}
			if tt.presenceErr == nil {
				require.Equal(t, int64(0), attrs["cordis.session.lease.presence_failure_count"])
			} else {
				require.Equal(t, int64(1), attrs["cordis.session.lease.presence_failure_count"])
			}
			telemetry := strings.ToLower(fmt.Sprint(cycle.Attributes, cycle.Events, cycle.Status))
			for _, sensitive := range []string{
				"sensitive-session-id", "sensitive-gateway-id", "sensitive-generation",
				"sensitive-device", "sensitive-owner-key", "sensitive-rpc-address",
			} {
				require.NotContains(t, telemetry, sensitive)
			}
		})
	}
}

type tracedLeaseStore struct {
	*fakeStore
	tracer trace.Tracer
	err    error
}

func (s *tracedLeaseStore) SetOwners(ctx context.Context, owners []store.Owner, ttl time.Duration) error {
	_, span := s.tracer.Start(ctx, "redis")
	defer span.End()
	if s.err != nil {
		return s.err
	}
	return s.fakeStore.SetOwners(ctx, owners, ttl)
}

type tracedLeasePresence struct {
	fakePresence
	tracer trace.Tracer
	err    error
}

func (p *tracedLeasePresence) RefreshUserSessions(
	ctx context.Context,
	_ *presencev1.RefreshUserSessionsRequest,
	_ ...grpc.CallOption,
) (*presencev1.RefreshUserSessionsResponse, error) {
	_, span := p.tracer.Start(ctx, "presence.refresh")
	defer span.End()
	if p.err != nil {
		return nil, p.err
	}
	return new(presencev1.RefreshUserSessionsResponse), nil
}

func leaseSpanByName(t *testing.T, spans tracetest.SpanStubs, name string) tracetest.SpanStub {
	t.Helper()
	for _, span := range spans {
		if span.Name == name {
			return span
		}
	}
	require.FailNow(t, "span not found", "name: %s", name)
	return tracetest.SpanStub{}
}

func leaseSpanAttributes(span tracetest.SpanStub) map[string]any {
	attrs := make(map[string]any, len(span.Attributes))
	for _, attr := range span.Attributes {
		attrs[string(attr.Key)] = attr.Value.AsInterface()
	}
	return attrs
}

func TestFilteredConnectPropagatesAndEndsHandshakeSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSyncer(exporter),
	)
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	filter := observability.ExcludeGRPCMethods(sessionv1.SessionService_Connect_FullMethodName)
	_, client := newTracedSessionClient(t, provider, filter)

	parentCtx, parent := provider.Tracer("test").Start(t.Context(), "gateway.session.bind")
	stream, err := client.Connect(parentCtx)
	require.NoError(t, err)
	first := new(sessionv1.ConnectRequest)
	first.SetConnectionId("sensitive-connection-id")
	first.SetGatewayId("sensitive-gateway-id")
	first.SetGatewayGeneration("sensitive-generation")
	identify := new(sessionv1.Identify)
	identify.SetToken("sensitive-access-token")
	first.SetIdentify(identify)
	require.NoError(t, stream.Send(first))
	ready, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, realtime.GatewayEventReady, ready.GetType())

	require.Eventually(t, func() bool { return len(exporter.GetSpans()) == 1 }, time.Second, time.Millisecond)
	span := exporter.GetSpans()[0]
	require.Equal(t, "session.identify", span.Name)
	require.Equal(t, parent.SpanContext().TraceID(), span.SpanContext.TraceID())
	require.Equal(t, parent.SpanContext().SpanID(), span.Parent.SpanID())
	attrs := make(map[string]string, len(span.Attributes))
	for _, attr := range span.Attributes {
		value := strings.ToLower(attr.Value.Emit())
		require.NotContains(t, value, "sensitive-")
		attrs[string(attr.Key)] = attr.Value.AsString()
	}
	require.Equal(t, map[string]string{
		"cordis.session.operation": "identify",
		"cordis.session.result":    "success",
	}, attrs)

	// The automatic Connect client/server spans remain absent while the stream is open.
	require.Len(t, exporter.GetSpans(), 1)
	detach := new(sessionv1.ConnectRequest)
	detach.SetConnectionId(first.GetConnectionId())
	detach.SetGatewayId(first.GetGatewayId())
	detach.SetGatewayGeneration(first.GetGatewayGeneration())
	detachData := new(sessionv1.Detach)
	detachData.SetResumable(true)
	detach.SetDetach(detachData)
	require.NoError(t, stream.Send(detach))
	parent.End()
}

func TestResumeSpanIncludesReplayAndEndsAfterResumed(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSyncer(exporter),
	)
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	filter := observability.ExcludeGRPCMethods(sessionv1.SessionService_Connect_FullMethodName)
	resumedSendStarted := make(chan struct{})
	releaseResumedSend := make(chan struct{})
	var releaseOnce sync.Once
	releaseResumed := func() { releaseOnce.Do(func() { close(releaseResumedSend) }) }
	defer releaseResumed()
	sessionServer, client := newTracedSessionClient(t, provider, filter, grpc.StreamInterceptor(
		func(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			return handler(srv, &blockingResumedServerStream{
				ServerStream: stream,
				started:      resumedSendStarted,
				release:      releaseResumedSend,
			})
		},
	))

	identify := new(sessionv1.Identify)
	identify.SetToken("token")
	logical, err := sessionServer.identify(t.Context(), "old-connection", "old-gateway", "old-generation", identify)
	require.NoError(t, err)
	logical.mu.Lock()
	oldBinding := logical.binding
	sessionServer.appendDispatchLocked(logical, realtime.EventMessageCreated, []byte(`{"id":"1"}`))
	logical.mu.Unlock()
	sessionServer.detach(logical, oldBinding, true)

	parentCtx, parent := provider.Tracer("test").Start(t.Context(), "gateway.session.bind")
	stream, err := client.Connect(parentCtx)
	require.NoError(t, err)
	first := new(sessionv1.ConnectRequest)
	first.SetConnectionId("new-connection")
	first.SetGatewayId("new-gateway")
	first.SetGatewayGeneration("new-generation")
	resume := new(sessionv1.Resume)
	resume.SetToken("token")
	resume.SetSessionId(logical.id)
	resume.SetSequence(1)
	first.SetResume(resume)
	require.NoError(t, stream.Send(first))

	replayed, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, realtime.EventMessageCreated, replayed.GetType())
	select {
	case <-resumedSendStarted:
	case <-time.After(time.Second):
		require.FailNow(t, "Session did not attempt the RESUMED write")
	}
	require.Empty(t, exporter.GetSpans(), "resume span ended before replay completed")
	releaseResumed()
	resumed, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, realtime.GatewayEventResumed, resumed.GetType())
	require.Eventually(t, func() bool { return len(exporter.GetSpans()) == 1 }, time.Second, time.Millisecond)
	span := exporter.GetSpans()[0]
	require.Equal(t, "session.resume", span.Name)
	require.Equal(t, parent.SpanContext().SpanID(), span.Parent.SpanID())

	detach := new(sessionv1.ConnectRequest)
	detach.SetConnectionId(first.GetConnectionId())
	detach.SetGatewayId(first.GetGatewayId())
	detach.SetGatewayGeneration(first.GetGatewayGeneration())
	detachData := new(sessionv1.Detach)
	detachData.SetResumable(true)
	detach.SetDetach(detachData)
	require.NoError(t, stream.Send(detach))
	parent.End()
}

func newTracedSessionClient(
	t *testing.T,
	provider *sdktrace.TracerProvider,
	filter otelgrpc.Filter,
	extraOptions ...grpc.ServerOption,
) (*Server, sessionv1.SessionServiceClient) {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	sessionServer := newTestServer()
	sessionServer.tracer = provider.Tracer(observability.SessionInstrumentationName)
	serverOptions := []grpc.ServerOption{grpc.StatsHandler(otelgrpc.NewServerHandler(
		otelgrpc.WithFilter(filter),
		otelgrpc.WithTracerProvider(provider),
		otelgrpc.WithPropagators(observability.RPCPropagator()),
	))}
	grpcServer := grpc.NewServer(append(serverOptions, extraOptions...)...)
	sessionv1.RegisterSessionServiceServer(grpcServer, sessionServer)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler(
			otelgrpc.WithFilter(filter),
			otelgrpc.WithTracerProvider(provider),
			otelgrpc.WithPropagators(observability.RPCPropagator()),
		)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	return sessionServer, sessionv1.NewSessionServiceClient(conn)
}

type blockingResumedServerStream struct {
	grpc.ServerStream
	started chan<- struct{}
	release <-chan struct{}
}

func (s *blockingResumedServerStream) SendMsg(message any) error {
	if frame, ok := message.(*sessionv1.ConnectResponse); ok && frame.GetType() == realtime.GatewayEventResumed {
		close(s.started)
		<-s.release
	}
	return s.ServerStream.SendMsg(message)
}
