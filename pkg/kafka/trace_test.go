package kafka

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestTraceContextMarshalRoundTrip(t *testing.T) {
	provider := sdktrace.NewTracerProvider()
	ctx, span := provider.Tracer("test").Start(context.Background(), "producer")
	spanContext := span.SpanContext()
	span.End()

	data := MarshalTraceContext(ctx)
	require.NotEmpty(t, data)

	record := new(kgo.Record)
	UnmarshalTraceContext(data, record)
	extracted := ExtractTraceContext(context.Background(), record)
	extractedContext := trace.SpanContextFromContext(extracted)
	require.True(t, extractedContext.IsRemote())
	require.Equal(t, spanContext.TraceID(), extractedContext.TraceID())
	require.Equal(t, spanContext.SpanID(), extractedContext.SpanID())
}

func TestUnmarshalTraceContextIgnoresInvalidData(t *testing.T) {
	record := new(kgo.Record)
	UnmarshalTraceContext([]byte("not-json"), record)
	require.Empty(t, record.Headers)
	UnmarshalTraceContext(nil, record)
	require.Empty(t, record.Headers)
}
