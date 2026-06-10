package observability

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type Config struct {
	Metrics MetricsConfig
	Tracing TracingConfig
}

type MetricsConfig struct {
	Enabled  bool   `json:",default=true"`
	ListenOn string `json:",default=0.0.0.0:6060"`
	Path     string `json:",default=/metrics"`
}

type TracingConfig struct {
	Disabled bool              `json:",optional"`
	Endpoint string            `json:",optional"`
	Sampler  float64           `json:",default=1.0"`
	Insecure bool              `json:",default=true"`
	Headers  map[string]string `json:",optional"`
}

type Runtime struct {
	metricsServer  *http.Server
	tracerProvider *sdktrace.TracerProvider
}

func SetUp(ctx context.Context, serviceName string, cfg Config) (*Runtime, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))

	tracerProvider, err := setUpTracing(ctx, serviceName, cfg.Tracing)
	if err != nil {
		return nil, err
	}

	metricsServer, err := startMetricsServer(cfg.Metrics)
	if err != nil {
		if tracerProvider != nil {
			_ = tracerProvider.Shutdown(ctx)
		}
		return nil, err
	}

	return &Runtime{
		metricsServer:  metricsServer,
		tracerProvider: tracerProvider,
	}, nil
}

func LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}

	var errs []error
	if r.metricsServer != nil {
		if err := r.metricsServer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown metrics server: %w", err))
		}
	}
	if r.tracerProvider != nil {
		if err := r.tracerProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown tracer provider: %w", err))
		}
	}
	return errors.Join(errs...)
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

func startMetricsServer(cfg MetricsConfig) (*http.Server, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if cfg.ListenOn == "" {
		cfg.ListenOn = "0.0.0.0:6060"
	}
	if cfg.Path == "" {
		cfg.Path = "/metrics"
	}

	mux := http.NewServeMux()
	mux.Handle("/health", LivenessHandler())
	mux.Handle(cfg.Path, promhttp.Handler())
	server := &http.Server{
		Addr:              cfg.ListenOn,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	listener, err := net.Listen("tcp", cfg.ListenOn)
	if err != nil {
		return nil, fmt.Errorf("listen metrics server %s: %w", cfg.ListenOn, err)
	}

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logx.Errorw("serve metrics",
				logx.Field("listen_on", cfg.ListenOn),
				logx.Field("error", err),
			)
		}
	}()
	return server, nil
}
