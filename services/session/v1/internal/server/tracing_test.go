package server

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/observability"
	"github.com/soasurs/cordis/pkg/realtime"
)

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
