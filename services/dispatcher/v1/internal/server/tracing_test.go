package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	cordiskafka "github.com/soasurs/cordis/pkg/kafka"
	"github.com/soasurs/cordis/pkg/observability"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/dispatcher/v1/config"
	"github.com/soasurs/cordis/services/dispatcher/v1/internal/discovery"
)

func TestDispatchAttemptsCreateSiblingConsumerSpans(t *testing.T) {
	exporter, provider := newTraceProvider(t)
	tracer := provider.Tracer("test")
	ctx, producerSpan := tracer.Start(t.Context(), "producer")
	producerContext := producerSpan.SpanContext()
	record := &kgo.Record{
		Topic:     "cordis.message.events.v1",
		Partition: 7,
		Key:       []byte("sensitive-key"),
		Value: []byte(`{"t":"` + realtime.EventMessageCreated +
			`","d":{"id":"9001","guild_id":"8001","channel_id":"7001"},"idempotency_key":"1"}`),
	}
	cordiskafka.InjectTraceContext(ctx, record)

	dispatcher := &Server{
		cfg:      config.Config{Kafka: config.KafkaConfig{ConsumerGroup: "cordis.dispatcher.v1"}},
		resolver: &fakeResolver{},
		tracer:   tracer,
	}
	for range 2 {
		permanent, err := dispatcher.dispatchRecord(t.Context(), record)
		require.NoError(t, err)
		require.False(t, permanent)
	}
	producerSpan.End()

	spans := exporter.GetSpans()
	consumers := spansByKind(spans, trace.SpanKindConsumer)
	require.Len(t, consumers, 2)
	for _, consumer := range consumers {
		require.Equal(t, producerContext.TraceID(), consumer.SpanContext.TraceID())
		require.Equal(t, producerContext.SpanID(), consumer.Parent.SpanID())
		require.Equal(t, "process cordis.message.events.v1", consumer.Name)
		attrs := attributesByKey(consumer.Attributes)
		require.Equal(t, "kafka", attrs["messaging.system"])
		require.Equal(t, "cordis.message.events.v1", attrs["messaging.destination.name"])
		require.Equal(t, "cordis.dispatcher.v1", attrs["messaging.consumer.group.name"])
		require.Equal(t, int64(7), attrs["messaging.destination.partition.id"])
		require.Equal(t, realtime.EventMessageCreated, attrs["cordis.event.type"])
		require.Equal(t, "success", attrs["cordis.messaging.result"])
		for key, value := range attrs {
			text := key + "=" + valueString(value)
			require.NotContains(t, text, "sensitive-key")
			require.NotContains(t, text, "9001")
			require.NotContains(t, text, "8001")
			require.NotContains(t, text, "7001")
		}
	}
	require.NotEqual(t, consumers[0].SpanContext.SpanID(), consumers[1].SpanContext.SpanID())
}

func TestDispatchPermanentFailureUsesBoundedErrorAttributes(t *testing.T) {
	exporter, provider := newTraceProvider(t)
	dispatcher := &Server{tracer: provider.Tracer("test")}
	record := &kgo.Record{Topic: "cordis.message.events.v1", Value: []byte("secret invalid payload")}

	permanent, err := dispatcher.dispatchRecord(t.Context(), record)
	require.Error(t, err)
	require.True(t, permanent)

	consumers := spansByKind(exporter.GetSpans(), trace.SpanKindConsumer)
	require.Len(t, consumers, 1)
	attrs := attributesByKey(consumers[0].Attributes)
	require.Equal(t, "permanent_failure", attrs["cordis.messaging.result"])
	require.Equal(t, "invalid_event", attrs["error.type"])
	for _, value := range attrs {
		require.NotContains(t, valueString(value), "secret invalid payload")
	}
}

func TestDispatchRetryableFailureUsesBoundedErrorAttributes(t *testing.T) {
	exporter, provider := newTraceProvider(t)
	dispatcher := &Server{resolver: failingResolver{}, tracer: provider.Tracer("test")}
	record := &kgo.Record{
		Topic: "cordis.message.events.v1",
		Value: []byte(`{"t":"` + realtime.EventMessageCreated +
			`","d":{"id":"9001","guild_id":"8001","channel_id":"7001"},"idempotency_key":"2"}`),
	}

	permanent, err := dispatcher.dispatchRecord(t.Context(), record)
	require.Error(t, err)
	require.False(t, permanent)

	consumer := requireSingleSpan(t, exporter.GetSpans(), trace.SpanKindConsumer)
	attrs := attributesByKey(consumer.Attributes)
	require.Equal(t, "retryable_failure", attrs["cordis.messaging.result"])
	require.Equal(t, "dispatch", attrs["error.type"])
	for _, value := range attrs {
		require.NotContains(t, valueString(value), "sensitive resolver detail")
	}
}

func TestDispatchPropagatesConsumerSpanToSessionRPC(t *testing.T) {
	exporter, provider := newTraceProvider(t)
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(observability.RPCPropagator())
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	grpcServer := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))
	sessionv1.RegisterSessionServiceServer(grpcServer, traceSessionServer{})
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)

	dispatcher := &Server{
		cfg:      config.Config{Dispatcher: config.DispatcherConfig{DispatchTimeoutSeconds: 2}},
		resolver: staticResolver{nodes: []discovery.Node{{ID: "node", Generation: "one", RPCAddress: listener.Addr().String()}}},
		tracer:   provider.Tracer(observability.DispatcherInstrumentationName),
		clients:  make(map[string]sessionv1.SessionServiceClient),
		conns:    make(map[string]*grpc.ClientConn),
	}
	t.Cleanup(func() {
		for _, conn := range dispatcher.conns {
			_ = conn.Close()
		}
	})

	ctx, producerSpan := provider.Tracer("producer").Start(t.Context(), "producer")
	record := &kgo.Record{
		Topic: "cordis.message.events.v1",
		Value: []byte(`{"t":"` + realtime.EventMessageCreated +
			`","d":{"id":"9001","guild_id":"8001","channel_id":"7001"},"idempotency_key":"3"}`),
	}
	cordiskafka.InjectTraceContext(ctx, record)
	permanent, err := dispatcher.dispatchRecord(t.Context(), record)
	require.NoError(t, err)
	require.False(t, permanent)
	producerSpan.End()

	spans := exporter.GetSpans()
	consumer := requireSingleSpan(t, spans, trace.SpanKindConsumer)
	client := requireSingleSpan(t, spans, trace.SpanKindClient)
	server := requireSingleSpan(t, spans, trace.SpanKindServer)
	require.Equal(t, consumer.SpanContext.TraceID(), client.SpanContext.TraceID())
	require.Equal(t, consumer.SpanContext.SpanID(), client.Parent.SpanID())
	require.Equal(t, client.SpanContext.TraceID(), server.SpanContext.TraceID())
	require.Equal(t, client.SpanContext.SpanID(), server.Parent.SpanID())
}

type staticResolver struct {
	nodes []discovery.Node
}

type failingResolver struct{}

func (failingResolver) Resolve(context.Context, discovery.RouteKind, int64) ([]discovery.Node, error) {
	return nil, errors.New("sensitive resolver detail")
}

func (r staticResolver) Resolve(context.Context, discovery.RouteKind, int64) ([]discovery.Node, error) {
	return r.nodes, nil
}

type traceSessionServer struct {
	sessionv1.UnimplementedSessionServiceServer
}

func (traceSessionServer) DispatchGuildMessageEvent(
	context.Context,
	*sessionv1.DispatchGuildMessageEventRequest,
) (*sessionv1.DispatchGuildMessageEventResponse, error) {
	return new(sessionv1.DispatchGuildMessageEventResponse), nil
}

func newTraceProvider(t *testing.T) (*tracetest.InMemoryExporter, *sdktrace.TracerProvider) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSyncer(exporter),
	)
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	return exporter, provider
}

func spansByKind(spans tracetest.SpanStubs, kind trace.SpanKind) tracetest.SpanStubs {
	var result tracetest.SpanStubs
	for _, span := range spans {
		if span.SpanKind == kind {
			result = append(result, span)
		}
	}
	return result
}

func requireSingleSpan(t *testing.T, spans tracetest.SpanStubs, kind trace.SpanKind) tracetest.SpanStub {
	t.Helper()
	matches := spansByKind(spans, kind)
	require.Len(t, matches, 1)
	return matches[0]
}

func attributesByKey(attributes []attribute.KeyValue) map[string]any {
	result := make(map[string]any, len(attributes))
	for _, attr := range attributes {
		result[string(attr.Key)] = attr.Value.AsInterface()
	}
	return result
}

func valueString(value any) string {
	return fmt.Sprint(value)
}
