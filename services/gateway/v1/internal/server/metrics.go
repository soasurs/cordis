package server

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	gatewaySessionStreamsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "cordis",
		Subsystem: "gateway",
		Name:      "session_streams_active",
		Help:      "Current Gateway connections with an established Session stream.",
	})
	gatewaySessionHandshakeDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cordis",
		Subsystem: "gateway",
		Name:      "session_handshake_duration_seconds",
		Help:      "Duration of Gateway to Session handshakes.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"operation", "result"})
	gatewayStreamFramesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cordis",
		Subsystem: "gateway",
		Name:      "stream_frames_total",
		Help:      "Frames transferred by Gateway streaming transports.",
	}, []string{"direction"})
	gatewayStreamFrameBytesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cordis",
		Subsystem: "gateway",
		Name:      "stream_frame_bytes_total",
		Help:      "Frame bytes transferred by Gateway streaming transports.",
	}, []string{"direction"})
	gatewaySessionReceiveFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cordis",
		Subsystem: "gateway",
		Name:      "session_receive_failures_total",
		Help:      "Failures while receiving frames from Session.",
	}, []string{"reason"})
	gatewayWebSocketWriteDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cordis",
		Subsystem: "gateway",
		Name:      "websocket_write_duration_seconds",
		Help:      "Duration of WebSocket frame writes.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"result"})
	gatewayWebSocketWriteFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cordis",
		Subsystem: "gateway",
		Name:      "websocket_write_failures_total",
		Help:      "Failures while writing WebSocket frames.",
	}, []string{"reason"})
	gatewayStreamClosesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cordis",
		Subsystem: "gateway",
		Name:      "stream_closes_total",
		Help:      "Closed Gateway streams by bounded reason class.",
	}, []string{"reason"})
)

func observeGatewayFrame(direction string, size int) {
	gatewayStreamFramesTotal.WithLabelValues(direction).Inc()
	gatewayStreamFrameBytesTotal.WithLabelValues(direction).Add(float64(size))
}

func gatewaySessionFailureReason(err error) string {
	if errors.Is(err, io.EOF) {
		return "eof"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	switch status.Code(err) {
	case codes.Unavailable:
		return "unavailable"
	case codes.ResourceExhausted:
		return "resource_exhausted"
	case codes.Unauthenticated, codes.PermissionDenied:
		return "rejected"
	default:
		return "internal"
	}
}

func gatewayWebSocketFailureReason(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	switch websocket.CloseStatus(err) {
	case websocket.StatusNormalClosure, websocket.StatusGoingAway:
		return "peer_closed"
	default:
		return "internal"
	}
}

func gatewayWebSocketCloseReason(ctx context.Context, err error, established bool) string {
	if ctx.Err() != nil {
		return "server_shutdown"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		if established {
			return "heartbeat_timeout"
		}
		return "handshake_timeout"
	}
	switch websocket.CloseStatus(err) {
	case websocket.StatusNormalClosure, websocket.StatusGoingAway, websocket.StatusNoStatusRcvd:
		return "peer_closed"
	default:
		return "websocket_error"
	}
}

func observeGatewayHandshake(start time.Time, operation string, err error) {
	gatewaySessionHandshakeDuration.WithLabelValues(operation, handshakeResult(err)).Observe(time.Since(start).Seconds())
}
