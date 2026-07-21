package observability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"
)

const filteredConnectMethod = "/session.v1.SessionService/Connect"

func TestExcludeGRPCMethods(t *testing.T) {
	filter := ExcludeGRPCMethods(filteredConnectMethod)
	require.False(t, filter(&stats.RPCTagInfo{FullMethodName: filteredConnectMethod}))
	require.True(t, filter(&stats.RPCTagInfo{FullMethodName: "/session.v1.SessionService/SyncGatewayConnections"}))
	require.True(t, filter(nil))
}

func TestFilteredGRPCMethodPropagatesWithoutAutomaticSpans(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSyncer(exporter),
	)
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	filter := ExcludeGRPCMethods(filteredConnectMethod)
	clientHandler := otelgrpc.NewClientHandler(
		otelgrpc.WithFilter(filter),
		otelgrpc.WithTracerProvider(provider),
		otelgrpc.WithPropagators(RPCPropagator()),
	)
	serverHandler := otelgrpc.NewServerHandler(
		otelgrpc.WithFilter(filter),
		otelgrpc.WithTracerProvider(provider),
		otelgrpc.WithPropagators(RPCPropagator()),
	)
	info := &stats.RPCTagInfo{FullMethodName: filteredConnectMethod}

	parentCtx, parent := provider.Tracer("test").Start(context.Background(), "gateway.session.bind")
	clientCtx := clientHandler.TagRPC(parentCtx, info)
	outgoing, ok := metadata.FromOutgoingContext(clientCtx)
	require.True(t, ok)
	require.NotEmpty(t, outgoing.Get("traceparent"))

	incoming := metadata.NewIncomingContext(context.Background(), outgoing)
	serverCtx := serverHandler.TagRPC(incoming, info)
	extracted := trace.SpanContextFromContext(serverCtx)
	require.True(t, extracted.IsValid())
	require.True(t, extracted.IsRemote())
	require.Equal(t, parent.SpanContext().TraceID(), extracted.TraceID())
	require.Equal(t, parent.SpanContext().SpanID(), extracted.SpanID())

	_, child := provider.Tracer("test").Start(serverCtx, "session.identify")
	child.End()
	parent.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 2)
	require.Equal(t, "session.identify", spans[0].Name)
	require.Equal(t, parent.SpanContext().SpanID(), spans[0].Parent.SpanID())
	require.Equal(t, "gateway.session.bind", spans[1].Name)
}
