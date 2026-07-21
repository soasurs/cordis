// Package observability defines tracing conventions shared by Cordis services.
package observability

import (
	"github.com/zeromicro/go-zero/core/trace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

const (
	// KafkaInstrumentationName identifies shared Kafka instrumentation.
	KafkaInstrumentationName = "github.com/soasurs/cordis/pkg/kafka"
	// DispatcherInstrumentationName identifies Dispatcher instrumentation.
	DispatcherInstrumentationName = "github.com/soasurs/cordis/services/dispatcher/v1"
)

// RPCPropagator returns the process-wide W3C propagator used by RPC transports.
func RPCPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

// KafkaPropagator returns the bounded W3C propagator used by Kafka records.
// Baggage is deliberately excluded so arbitrary request baggage is not copied
// onto every event.
func KafkaPropagator() propagation.TextMapPropagator {
	return propagation.TraceContext{}
}

// StartTracing configures RPC propagation and starts go-zero tracing.
func StartTracing(serviceName string, cfg trace.Config) {
	otel.SetTextMapPropagator(RPCPropagator())
	if cfg.Name == "" {
		cfg.Name = serviceName
	}
	trace.StartAgent(cfg)
}

// StopTracing flushes and shuts down go-zero tracing.
func StopTracing() {
	trace.StopAgent()
}
