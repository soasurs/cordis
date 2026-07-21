package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/soasurs/cordis/pkg/observability"
	"github.com/soasurs/cordis/services/gateway/v1/config"
	"github.com/soasurs/cordis/services/gateway/v1/internal/svc"
)

func TestHandshakeSpanEndsWhileWebSocketRemainsOpen(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSyncer(exporter),
	)
	previousProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		require.NoError(t, provider.Shutdown(context.Background()))
	})

	sessionAddress := startFakeSessionServer(t)
	gateway := New(svc.NewServiceContextWithDependencies(config.Config{
		Name:     "gateway.test",
		ListenOn: "127.0.0.1:8081",
		Gateway: config.GatewayConfig{
			WebSocketPath:          "/ws",
			HeartbeatIntervalMs:    50,
			IdentifyTimeoutSeconds: 1,
		},
	}, svc.Dependencies{
		Resolver: fakeResolver{address: sessionAddress},
	}))
	gateway.tracer = provider.Tracer(observability.GatewayInstrumentationName)

	conn, reader := connectWebSocket(t, gateway, "/ws")
	defer conn.Close()
	_ = readEnvelope(t, reader)
	writeClientText(t, conn, `{"op":2,"d":{"token":"secret-access-token"}}`)
	require.Equal(t, eventReady, readEnvelope(t, reader).T)

	require.Eventually(t, func() bool { return len(exporter.GetSpans()) == 1 }, time.Second, time.Millisecond)
	span := exporter.GetSpans()[0]
	require.Equal(t, "gateway.session.bind", span.Name)
	attrs := make(map[string]string, len(span.Attributes))
	for _, attr := range span.Attributes {
		attrs[string(attr.Key)] = attr.Value.AsString()
		require.NotContains(t, strings.ToLower(attr.Value.Emit()), "secret-access-token")
	}
	require.Equal(t, "identify", attrs["cordis.session.operation"])
	require.Equal(t, "success", attrs["cordis.session.result"])

	// A post-handshake heartbeat proves the stream outlives the exported span.
	time.Sleep(gateway.svcCtx.Cfg.Gateway.HeartbeatMinimumInterval())
	writeClientText(t, conn, `{"op":1,"d":1}`)
	require.Equal(t, eventHeartbeatAck, readEnvelope(t, reader).T)
	require.Len(t, exporter.GetSpans(), 1)
}
