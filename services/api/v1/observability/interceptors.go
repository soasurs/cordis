package observability

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/soasurs/cordis/pkg/apierror"
)

const (
	connectMetricNamespace = "connect_server"
	traceName              = "connect"
)

var (
	connectRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: connectMetricNamespace,
			Subsystem: "requests",
			Name:      "duration_ms",
			Help:      "Connect server requests duration(ms).",
			Buckets:   []float64{1, 2, 5, 10, 25, 50, 100, 250, 500, 1000, 2000, 5000},
		},
		[]string{"procedure", "code", "public_code"},
	)

	connectRequestCodeTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: connectMetricNamespace,
			Subsystem: "requests",
			Name:      "code_total",
			Help:      "Connect server requests code count.",
		},
		[]string{"procedure", "code", "public_code"},
	)
)

func ConnectInterceptors() []connect.Interceptor {
	return []connect.Interceptor{
		UnaryTracingInterceptor(),
		UnaryPrometheusInterceptor(),
		UnaryErrorLogInterceptor(),
	}
}

func UnaryTracingInterceptor() connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			spec := req.Spec()
			ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(req.Header()))

			ctx, span := otel.Tracer(traceName).Start(ctx, spanName(spec.Procedure),
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(connectSpanAttributes(req)...),
			)
			defer span.End()

			resp, err := next(ctx, req)
			code, publicCode := errorCodes(err)
			span.SetAttributes(
				attribute.String("connect.code", code),
				attribute.String("error.public_code", publicCode),
			)
			if err != nil {
				span.SetStatus(otelcodes.Error, code)
			}
			return resp, err
		}
	})
}

func UnaryPrometheusInterceptor() connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			start := time.Now()
			resp, err := next(ctx, req)
			code, publicCode := errorCodes(err)
			procedure := req.Spec().Procedure
			connectRequestDuration.WithLabelValues(procedure, code, publicCode).Observe(float64(time.Since(start).Milliseconds()))
			connectRequestCodeTotal.WithLabelValues(procedure, code, publicCode).Inc()
			return resp, err
		}
	})
}

func UnaryErrorLogInterceptor() connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			start := time.Now()
			resp, err := next(ctx, req)
			if err != nil && shouldLogConnectError(connect.CodeOf(err)) {
				code, publicCode := errorCodes(err)
				logx.WithContext(ctx).Errorw("connect request failed",
					logx.Field("procedure", req.Spec().Procedure),
					logx.Field("connect_code", code),
					logx.Field("public_code", publicCode),
					logx.Field("duration_ms", time.Since(start).Milliseconds()),
				)
			}
			return resp, err
		}
	})
}

func shouldLogConnectError(code connect.Code) bool {
	switch code {
	case connect.CodeInternal,
		connect.CodeUnknown,
		connect.CodeUnavailable,
		connect.CodeDataLoss,
		connect.CodeDeadlineExceeded:
		return true
	default:
		return false
	}
}

func errorCodes(err error) (string, string) {
	if err == nil {
		return "ok", ""
	}
	return connect.CodeOf(err).String(), apierror.PublicCode(err)
}

func connectSpanAttributes(req connect.AnyRequest) []attribute.KeyValue {
	spec := req.Spec()
	service, method := splitProcedure(spec.Procedure)
	attrs := []attribute.KeyValue{
		attribute.String("rpc.system", "connect_rpc"),
		attribute.String("rpc.service", service),
		attribute.String("rpc.method", method),
		attribute.String("connect.procedure", spec.Procedure),
		attribute.String("connect.protocol", req.Peer().Protocol),
	}
	if peerAddr := req.Peer().Addr; peerAddr != "" {
		attrs = append(attrs, attribute.String("net.peer.address", peerAddr))
	}
	return attrs
}

func spanName(procedure string) string {
	return strings.TrimPrefix(procedure, "/")
}

func splitProcedure(procedure string) (string, string) {
	parts := strings.Split(strings.TrimPrefix(procedure, "/"), "/")
	if len(parts) != 2 {
		return procedure, ""
	}
	return parts[0], parts[1]
}
