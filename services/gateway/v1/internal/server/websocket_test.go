package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/gateway/v1/config"
	"github.com/soasurs/cordis/services/gateway/v1/internal/svc"
)

func TestWebSocketForwardsSessionFrames(t *testing.T) {
	sessionAddress := startFakeSessionServer(t)
	socketLimiter := &gatewayFakeSocketLimiter{allowed: true}
	gateway := New(svc.NewServiceContextWithDependencies(config.Config{
		Name:     "gateway.test",
		ListenOn: "127.0.0.1:8081",
		Gateway: config.GatewayConfig{
			WebSocketPath:          "/ws",
			HeartbeatIntervalMs:    50,
			IdentifyTimeoutSeconds: 1,
		},
	}, svc.Dependencies{
		Resolver:      fakeResolver{address: sessionAddress},
		SocketLimiter: socketLimiter,
	}))

	conn, reader := connectWebSocket(t, gateway, "/ws")
	defer conn.Close()

	hello := readEnvelope(t, reader)
	require.Equal(t, opHello, hello.Op)
	require.Equal(t, eventHello, hello.T)

	writeClientText(t, conn, `{"op":2,"d":{"token":"access-token","device_type":"desktop"}}`)
	ready := readEnvelope(t, reader)
	require.Equal(t, opDispatch, ready.Op)
	require.Equal(t, eventReady, ready.T)
	require.Equal(t, uint64(1), ready.S)
	require.Eventually(t, socketLimiter.lease.ready.Load, time.Second, time.Millisecond)

	writeClientText(t, conn, `{"op":1,"d":1}`)
	early := readEnvelope(t, reader)
	require.Equal(t, opError, early.Op)
	require.Contains(t, string(early.D), "before negotiated interval")
	time.Sleep(gateway.svcCtx.Cfg.Gateway.HeartbeatMinimumInterval())
	writeClientText(t, conn, `{"op":1,"d":1}`)
	ack := readEnvelope(t, reader)
	require.Equal(t, opHeartbeatAck, ack.Op)
}

func TestWebSocketRejectsMissingHandshake(t *testing.T) {
	gateway := New(svc.NewServiceContextWithDependencies(config.Config{
		ListenOn: "127.0.0.1:8081",
		Gateway: config.GatewayConfig{
			WebSocketPath:          "/ws",
			IdentifyTimeoutSeconds: 1,
		},
	}, svc.Dependencies{
		Resolver: fakeResolver{address: startFakeSessionServer(t)},
	}))
	conn, reader := connectWebSocket(t, gateway, "/ws")
	defer conn.Close()
	_ = readEnvelope(t, reader)

	writeClientText(t, conn, `{"op":1,"d":0}`)
	failure := readEnvelope(t, reader)
	require.Equal(t, opError, failure.Op)
	require.Equal(t, eventError, failure.T)
}

func TestRealWebSocketClientLifecycle(t *testing.T) {
	sessionAddress := startFakeSessionServer(t)
	gateway := New(svc.NewServiceContextWithDependencies(config.Config{
		Name:     "gateway.test",
		ListenOn: "127.0.0.1:8081",
		Gateway: config.GatewayConfig{
			WebSocketPath:          "/ws",
			HeartbeatIntervalMs:    50,
			IdentifyTimeoutSeconds: 5,
		},
	}, svc.Dependencies{
		Resolver: fakeResolver{address: sessionAddress},
	}))

	httpSrv := httptest.NewServer(gateway.Handler())
	defer httpSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	ctx := t.Context()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	require.NoError(t, err)
	defer conn.CloseNow()

	var hello envelope
	require.NoError(t, wsjson.Read(ctx, conn, &hello))
	require.Equal(t, opHello, hello.Op)
	require.Equal(t, eventHello, hello.T)

	require.NoError(t, wsjson.Write(ctx, conn, envelope{
		Op: opIdentify,
		T:  realtime.GatewayEventIdentify,
		D:  json.RawMessage(`{"token":"access-token"}`),
	}))
	var ready envelope
	require.NoError(t, wsjson.Read(ctx, conn, &ready))
	require.Equal(t, opDispatch, ready.Op)
	require.Equal(t, eventReady, ready.T)
	require.Equal(t, uint64(1), ready.S)

	time.Sleep(gateway.svcCtx.Cfg.Gateway.HeartbeatMinimumInterval())
	require.NoError(t, wsjson.Write(ctx, conn, envelope{
		Op: opHeartbeat,
		T:  realtime.GatewayEventHeartbeat,
		D:  json.RawMessage(`1`),
	}))
	var ack envelope
	require.NoError(t, wsjson.Read(ctx, conn, &ack))
	require.Equal(t, opHeartbeatAck, ack.Op)
}

func TestWebSocketHeartbeatTimeoutIsGatewayLocal(t *testing.T) {
	sessionAddress := startFakeSessionServer(t)
	gateway := New(svc.NewServiceContextWithDependencies(config.Config{
		Name:     "gateway.test",
		ListenOn: "127.0.0.1:8081",
		Gateway: config.GatewayConfig{
			WebSocketPath:             "/ws",
			HeartbeatIntervalMs:       25,
			HeartbeatTimeoutIntervals: 2,
			IdentifyTimeoutSeconds:    1,
		},
	}, svc.Dependencies{
		Resolver: fakeResolver{address: sessionAddress},
	}))

	httpSrv := httptest.NewServer(gateway.Handler())
	defer httpSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"
	conn, _, err := websocket.Dial(t.Context(), wsURL, nil)
	require.NoError(t, err)
	defer conn.CloseNow()

	var message envelope
	require.NoError(t, wsjson.Read(t.Context(), conn, &message))
	require.NoError(t, wsjson.Write(t.Context(), conn, envelope{
		Op: opIdentify, D: json.RawMessage(`{"token":"access-token"}`),
	}))
	require.NoError(t, wsjson.Read(t.Context(), conn, &message))

	readCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err = wsjson.Read(readCtx, conn, &message)
	require.Error(t, err)
}

func TestWebSocketResumeLifecycle(t *testing.T) {
	sessionAddress := startFakeSessionServer(t)
	gateway := New(svc.NewServiceContextWithDependencies(config.Config{
		Name:     "gateway.test",
		ListenOn: "127.0.0.1:8081",
		Gateway: config.GatewayConfig{
			WebSocketPath:          "/ws",
			HeartbeatIntervalMs:    50,
			IdentifyTimeoutSeconds: 5,
		},
	}, svc.Dependencies{
		Resolver: fakeResolver{address: sessionAddress},
	}))

	conn, reader := connectWebSocket(t, gateway, "/ws")
	defer conn.Close()

	hello := readEnvelope(t, reader)
	require.Equal(t, opHello, hello.Op)

	writeClientText(t, conn, fmt.Sprintf(
		`{"op":6,"t":%q,"d":{"token":"access-token","session_id":"sess-1","seq":42}}`,
		realtime.GatewayEventResume,
	))
	resumed := readEnvelope(t, reader)
	require.Equal(t, opDispatch, resumed.Op)
	require.Equal(t, eventResumed, resumed.T)
	require.Equal(t, uint64(100), resumed.S)
}

func TestWebSocketPresenceUpdate(t *testing.T) {
	sessionAddress := startFakeSessionServer(t)
	gateway := New(svc.NewServiceContextWithDependencies(config.Config{
		Name:     "gateway.test",
		ListenOn: "127.0.0.1:8081",
		Gateway: config.GatewayConfig{
			WebSocketPath:          "/ws",
			HeartbeatIntervalMs:    50,
			IdentifyTimeoutSeconds: 5,
		},
	}, svc.Dependencies{
		Resolver: fakeResolver{address: sessionAddress},
	}))

	conn, reader := connectWebSocket(t, gateway, "/ws")
	defer conn.Close()

	_ = readEnvelope(t, reader)
	writeClientText(t, conn, `{"op":2,"d":{"token":"access-token"}}`)
	_ = readEnvelope(t, reader)

	writeClientText(t, conn, `{"op":3,"d":{"status":"online","client_state":"foreground"}}`)
	time.Sleep(gateway.svcCtx.Cfg.Gateway.HeartbeatMinimumInterval())
	writeClientText(t, conn, `{"op":1,"d":1}`)
	ack := readEnvelope(t, reader)
	require.Equal(t, opHeartbeatAck, ack.Op)
}

func TestWebSocketDetachOnClose(t *testing.T) {
	sessionAddress := startFakeSessionServer(t)
	gateway := New(svc.NewServiceContextWithDependencies(config.Config{
		Name:     "gateway.test",
		ListenOn: "127.0.0.1:8081",
		Gateway: config.GatewayConfig{
			WebSocketPath:          "/ws",
			HeartbeatIntervalMs:    50,
			IdentifyTimeoutSeconds: 5,
		},
	}, svc.Dependencies{
		Resolver: fakeResolver{address: sessionAddress},
	}))

	conn, reader := connectWebSocket(t, gateway, "/ws")

	_ = readEnvelope(t, reader)
	writeClientText(t, conn, `{"op":2,"d":{"token":"access-token"}}`)
	_ = readEnvelope(t, reader)

	_ = conn.Close()

	// After the client connection closes, the gateway run loop exits and
	// calls close(). Read should eventually return io.EOF or an error
	// since the pipe is closed.
	_, err := reader.ReadByte()
	require.Error(t, err)
}

func TestWebSocketInvalidOpcode(t *testing.T) {
	sessionAddress := startFakeSessionServer(t)
	gateway := New(svc.NewServiceContextWithDependencies(config.Config{
		Name:     "gateway.test",
		ListenOn: "127.0.0.1:8081",
		Gateway: config.GatewayConfig{
			WebSocketPath:          "/ws",
			HeartbeatIntervalMs:    50,
			IdentifyTimeoutSeconds: 5,
		},
	}, svc.Dependencies{
		Resolver: fakeResolver{address: sessionAddress},
	}))

	conn, reader := connectWebSocket(t, gateway, "/ws")
	defer conn.Close()

	_ = readEnvelope(t, reader)
	writeClientText(t, conn, `{"op":2,"d":{"token":"access-token"}}`)
	_ = readEnvelope(t, reader)

	writeClientText(t, conn, `{"op":99,"d":{}}`)
	errMsg := readEnvelope(t, reader)
	require.Equal(t, opError, errMsg.Op)
	require.Equal(t, eventError, errMsg.T)
}

func TestWebSocketIdentifyFailsWhenResolverReturnsError(t *testing.T) {
	gateway := New(svc.NewServiceContextWithDependencies(config.Config{
		Name:     "gateway.test",
		ListenOn: "127.0.0.1:8081",
		Gateway: config.GatewayConfig{
			WebSocketPath:          "/ws",
			HeartbeatIntervalMs:    50,
			IdentifyTimeoutSeconds: 5,
		},
	}, svc.Dependencies{
		Resolver: fakeResolver{err: fmt.Errorf("ready session node not found")},
	}))

	conn, reader := connectWebSocket(t, gateway, "/ws")
	defer conn.Close()

	_ = readEnvelope(t, reader)
	writeClientText(t, conn, `{"op":2,"d":{"token":"access-token"}}`)
	failure := readEnvelope(t, reader)
	require.Equal(t, opError, failure.Op)
	require.Equal(t, eventError, failure.T)
}

func TestWebSocketResumeFailsWhenResolverReturnsError(t *testing.T) {
	gateway := New(svc.NewServiceContextWithDependencies(config.Config{
		Name:     "gateway.test",
		ListenOn: "127.0.0.1:8081",
		Gateway: config.GatewayConfig{
			WebSocketPath:          "/ws",
			HeartbeatIntervalMs:    50,
			IdentifyTimeoutSeconds: 5,
		},
	}, svc.Dependencies{
		Resolver: fakeResolver{err: fmt.Errorf("session owner not found")},
	}))

	conn, reader := connectWebSocket(t, gateway, "/ws")
	defer conn.Close()

	_ = readEnvelope(t, reader)
	writeClientText(t, conn, `{"op":6,"d":{"token":"access-token","session_id":"sess-1","seq":42}}`)
	invalid := readEnvelope(t, reader)
	require.Equal(t, opInvalid, invalid.Op)
}

func TestWebSocketResumeEmptySessionID(t *testing.T) {
	sessionAddress := startFakeSessionServer(t)
	gateway := New(svc.NewServiceContextWithDependencies(config.Config{
		Name:     "gateway.test",
		ListenOn: "127.0.0.1:8081",
		Gateway: config.GatewayConfig{
			WebSocketPath:          "/ws",
			HeartbeatIntervalMs:    50,
			IdentifyTimeoutSeconds: 5,
		},
	}, svc.Dependencies{
		Resolver: fakeResolver{address: sessionAddress},
	}))

	conn, reader := connectWebSocket(t, gateway, "/ws")
	defer conn.Close()

	_ = readEnvelope(t, reader)
	writeClientText(t, conn, `{"op":6,"d":{"token":"access-token","session_id":"  ","seq":0}}`)
	invalid := readEnvelope(t, reader)
	require.Equal(t, opInvalid, invalid.Op)
}

func TestWebSocketIdentifyTimeout(t *testing.T) {
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

	_, reader := connectWebSocket(t, gateway, "/ws")

	_ = readEnvelope(t, reader)
	time.Sleep(1500 * time.Millisecond)

	// The gateway closes the websocket after identify timeout, so any
	// subsequent read should fail.
	_, err := reader.ReadByte()
	require.Error(t, err)
}
