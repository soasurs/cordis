package kafka

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/baggage"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestTraceContextRoundTripExcludesBaggage(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSyncer(exporter),
	)
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	member, err := baggage.NewMember("tenant", "secret")
	require.NoError(t, err)
	bag, err := baggage.New(member)
	require.NoError(t, err)
	ctx := baggage.ContextWithBaggage(t.Context(), bag)
	ctx, producerSpan := provider.Tracer("test").Start(ctx, "producer")
	producerContext := producerSpan.SpanContext()

	record := &kgo.Record{Headers: []kgo.RecordHeader{
		{Key: "traceparent", Value: []byte("stale")},
		{Key: "TraceParent", Value: []byte("duplicate")},
		{Key: "application", Value: []byte("preserved")},
	}}
	InjectTraceContext(ctx, record)
	producerSpan.End()

	require.Equal(t, 1, countHeader(record.Headers, "traceparent"))
	require.Equal(t, 0, countHeader(record.Headers, "baggage"))
	require.Equal(t, "preserved", NewRecordHeaderCarrier(&record.Headers).Get("application"))

	extracted := ExtractTraceContext(t.Context(), record)
	extractedContext := trace.SpanContextFromContext(extracted)
	require.True(t, extractedContext.IsRemote())
	require.Equal(t, producerContext.TraceID(), extractedContext.TraceID())
	require.Equal(t, producerContext.SpanID(), extractedContext.SpanID())
	require.Equal(t, producerContext.TraceFlags(), extractedContext.TraceFlags())
	require.Empty(t, baggage.FromContext(extracted).Members())
}

func TestPublisherInjectsTraceContextWithoutProducerSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	ctx, span := provider.Tracer("test").Start(t.Context(), "rpc.server")
	producer := new(capturingProducer)
	publisher := &Publisher{producer: producer, topic: "events"}
	require.NoError(t, publisher.Publish(ctx, []byte("key"), []byte("payload")))
	span.End()

	require.NotNil(t, producer.record)
	require.Equal(t, "events", producer.record.Topic)
	require.Equal(t, []byte("key"), producer.record.Key)
	require.Equal(t, []byte("payload"), producer.record.Value)
	require.NotEmpty(t, NewRecordHeaderCarrier(&producer.record.Headers).Get("traceparent"))
	require.Len(t, exporter.GetSpans(), 1)
}

type capturingProducer struct {
	record *kgo.Record
}

func (p *capturingProducer) ProduceSync(_ context.Context, records ...*kgo.Record) kgo.ProduceResults {
	p.record = records[0]
	return kgo.ProduceResults{{Record: records[0]}}
}

func countHeader(headers []kgo.RecordHeader, key string) int {
	count := 0
	for _, header := range headers {
		if strings.EqualFold(header.Key, key) {
			count++
		}
	}
	return count
}
