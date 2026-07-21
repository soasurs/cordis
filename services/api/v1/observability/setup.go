package observability

import (
	"context"
	"fmt"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type Config struct {
	Tracing TracingConfig
}

type TracingConfig struct {
	Disabled bool              `json:",optional"`
	Endpoint string            `json:",optional"`
	Sampler  float64           `json:",default=1.0"`
	Insecure bool              `json:",default=true"`
	Headers  map[string]string `json:",optional"`
}

type Runtime struct {
	tracerProvider *sdktrace.TracerProvider
}

func SetUp(ctx context.Context, serviceName string, cfg Config) (*Runtime, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))

	tracerProvider, err := setUpTracing(ctx, serviceName, cfg.Tracing)
	if err != nil {
		return nil, err
	}

	return &Runtime{
		tracerProvider: tracerProvider,
	}, nil
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}

	if r.tracerProvider != nil {
		return r.tracerProvider.Shutdown(ctx)
	}
	return nil
}

func setUpTracing(ctx context.Context, serviceName string, cfg TracingConfig) (*sdktrace.TracerProvider, error) {
	if cfg.Disabled {
		return nil, nil
	}
	if cfg.Sampler < 0 || cfg.Sampler > 1 {
		return nil, fmt.Errorf("observability tracing sampler must be between 0 and 1: %v", cfg.Sampler)
	}

	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.Sampler))),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", serviceName))),
	}

	if cfg.Endpoint != "" {
		exporterOpts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(cfg.Endpoint),
		}
		if cfg.Insecure {
			exporterOpts = append(exporterOpts, otlptracegrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			exporterOpts = append(exporterOpts, otlptracegrpc.WithHeaders(cfg.Headers))
		}
		exporter, err := otlptracegrpc.New(ctx, exporterOpts...)
		if err != nil {
			return nil, fmt.Errorf("create otlp trace exporter: %w", err)
		}
		opts = append(opts, sdktrace.WithBatcher(exporter))
	}

	provider := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(provider)
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logx.Errorw("otel error", logx.Field("error", err))
	}))
	return provider, nil
}
