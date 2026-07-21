package server

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	sessionGatewayStreamsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "cordis",
		Subsystem: "session",
		Name:      "gateway_streams_active",
		Help:      "Current Gateway streams connected to Session.",
	})
	sessionGatewayHandshakeDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cordis",
		Subsystem: "session",
		Name:      "gateway_handshake_duration_seconds",
		Help:      "Duration of Session identify and resume handshakes.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"operation", "result"})
	sessionGatewayFramesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cordis",
		Subsystem: "session",
		Name:      "gateway_frames_total",
		Help:      "Frames transferred between Session and Gateway.",
	}, []string{"direction"})
	sessionGatewayFrameBytesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cordis",
		Subsystem: "session",
		Name:      "gateway_frame_bytes_total",
		Help:      "Frame bytes transferred between Session and Gateway.",
	}, []string{"direction"})
	sessionBindingQueueOverflowsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "cordis",
		Subsystem: "session",
		Name:      "binding_queue_overflows_total",
		Help:      "Session binding sends rejected because the queue was full.",
	})
	sessionGatewayStreamClosesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cordis",
		Subsystem: "session",
		Name:      "gateway_stream_closes_total",
		Help:      "Closed Session Gateway streams by bounded reason class.",
	}, []string{"reason"})
)

func observeSessionGatewayFrame(direction string, size int) {
	sessionGatewayFramesTotal.WithLabelValues(direction).Inc()
	sessionGatewayFrameBytesTotal.WithLabelValues(direction).Add(float64(size))
}

func observeSessionHandshake(start time.Time, operation string, err error) {
	sessionGatewayHandshakeDuration.WithLabelValues(operation, sessionHandshakeResult(err)).Observe(time.Since(start).Seconds())
}

func sessionStreamCloseReason(err error) string {
	if err == nil {
		return "peer_detached"
	}
	if errors.Is(err, io.EOF) {
		return "peer_closed"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	switch status.Code(err) {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.NotFound,
		codes.Unauthenticated, codes.PermissionDenied, codes.ResourceExhausted:
		return "rejected"
	case codes.Unavailable:
		return "unavailable"
	default:
		return "internal"
	}
}
