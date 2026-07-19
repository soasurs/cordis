package server

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/services/gateway/v1/config"
	"github.com/soasurs/cordis/services/gateway/v1/internal/discovery"
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
	require.True(t, socketLimiter.lease.ready.Load())

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

func TestToGatewayFrameParsesStringChannelIDs(t *testing.T) {
	client := &client{connectionID: "connection-test", server: &Server{gatewayID: "gateway-test"}}
	frame, err := client.toGatewayFrame(envelope{
		Op: opSubscribe,
		D:  json.RawMessage(`{"channel_ids":["9007199254740993"]}`),
	})
	require.NoError(t, err)
	require.Equal(t, []int64{9007199254740993}, frame.GetSubscribe().GetChannelIds())
}

func TestToGatewayFrameRejectsNumericChannelIDs(t *testing.T) {
	client := &client{connectionID: "connection-test", server: &Server{gatewayID: "gateway-test"}}
	_, err := client.toGatewayFrame(envelope{
		Op: opSubscribe,
		D:  json.RawMessage(`{"channel_ids":[9007199254740993]}`),
	})
	require.Error(t, err)
}

func TestToGatewayFramePresenceUpdate(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1", generation: "gen-1"}}
	frame, err := client.toGatewayFrame(envelope{
		Op: opPresence,
		D:  json.RawMessage(`{"status":"online","client_state":"mobile"}`),
	})
	require.NoError(t, err)
	require.Equal(t, "conn-1", frame.GetConnectionId())
	require.Equal(t, "gw-1", frame.GetGatewayId())
	require.Equal(t, "gen-1", frame.GetGatewayGeneration())
	require.Equal(t, "online", frame.GetPresence().GetStatus())
	require.Equal(t, "mobile", frame.GetPresence().GetClientState())
}

func TestToGatewayFrameResume(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1", generation: "gen-1"}}
	frame, err := client.toGatewayFrame(envelope{
		Op: opResume,
		D:  json.RawMessage(`{"token":"access-token","session_id":"sess-1","seq":42}`),
	})
	require.NoError(t, err)
	require.Equal(t, "access-token", frame.GetResume().GetToken())
	require.Equal(t, "sess-1", frame.GetResume().GetSessionId())
	require.Equal(t, uint64(42), frame.GetResume().GetSequence())
}

func TestToGatewayFrameResumeInvalidJSON(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	_, err := client.toGatewayFrame(envelope{
		Op: opResume,
		D:  json.RawMessage(`invalid`),
	})
	require.Error(t, err)
}

func TestToGatewayFrameIdentifyInvalidJSON(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	_, err := client.toGatewayFrame(envelope{
		Op: opIdentify,
		D:  json.RawMessage(`invalid`),
	})
	require.Error(t, err)
}

func TestToGatewayFrameSubscribeEmptyChannelIDs(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	_, err := client.toGatewayFrame(envelope{
		Op: opSubscribe,
		D:  json.RawMessage(`{"channel_ids":[]}`),
	})
	require.NoError(t, err)
}

func TestToGatewayFrameSubscribeInvalidID(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	_, err := client.toGatewayFrame(envelope{
		Op: opSubscribe,
		D:  json.RawMessage(`{"channel_ids":["abc"]}`),
	})
	require.Error(t, err)
}

func TestToGatewayFrameSubscribeZeroID(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	_, err := client.toGatewayFrame(envelope{
		Op: opSubscribe,
		D:  json.RawMessage(`{"channel_ids":["0"]}`),
	})
	require.Error(t, err)
}

func TestToGatewayFrameSubscribeNegativeID(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	_, err := client.toGatewayFrame(envelope{
		Op: opSubscribe,
		D:  json.RawMessage(`{"channel_ids":["-1"]}`),
	})
	require.Error(t, err)
}

func TestToGatewayFrameSubscribeInvalidJSON(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	_, err := client.toGatewayFrame(envelope{
		Op: opSubscribe,
		D:  json.RawMessage(`invalid`),
	})
	require.Error(t, err)
}

func TestToGatewayFrameHeartbeatNullD(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	frame, err := client.toGatewayFrame(envelope{
		Op: opHeartbeat,
		D:  json.RawMessage(`null`),
	})
	require.NoError(t, err)
	require.NotNil(t, frame.GetHeartbeat())
	require.Equal(t, uint64(0), frame.GetHeartbeat().GetSequence())
}

func TestToGatewayFrameHeartbeatEmptyD(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	frame, err := client.toGatewayFrame(envelope{
		Op: opHeartbeat,
	})
	require.NoError(t, err)
	require.NotNil(t, frame.GetHeartbeat())
	require.Equal(t, uint64(0), frame.GetHeartbeat().GetSequence())
}

func TestToGatewayFrameHeartbeatWithSequence(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	frame, err := client.toGatewayFrame(envelope{
		Op: opHeartbeat,
		D:  json.RawMessage(`42`),
	})
	require.NoError(t, err)
	require.Equal(t, uint64(42), frame.GetHeartbeat().GetSequence())
}

func TestToGatewayFrameHeartbeatInvalidD(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	_, err := client.toGatewayFrame(envelope{
		Op: opHeartbeat,
		D:  json.RawMessage(`{"x":1}`),
	})
	require.Error(t, err)
}

func TestToGatewayFrameUnknownOpcode(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	_, err := client.toGatewayFrame(envelope{
		Op: 99,
		D:  json.RawMessage(`{}`),
	})
	require.Error(t, err)
}

func TestToGatewayFramePresenceInvalidJSON(t *testing.T) {
	client := &client{connectionID: "conn-1", server: &Server{gatewayID: "gw-1"}}
	_, err := client.toGatewayFrame(envelope{
		Op: opPresence,
		D:  json.RawMessage(`invalid`),
	})
	require.Error(t, err)
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

type fakeSessionServer struct {
	sessionv1.UnimplementedSessionServiceServer
	name string
}

func (s fakeSessionServer) Connect(stream sessionv1.SessionService_ConnectServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	switch {
	case first.GetIdentify() != nil:
		ready := new(sessionv1.ConnectResponse)
		ready.SetOpcode(opDispatch)
		ready.SetSequence(1)
		ready.SetType(eventReady)
		ready.SetJsonPayload(`{"session_id":"sess-test-` + s.name + `"}`)
		ready.SetSessionId("sess-test-" + s.name)
		ready.SetBindingEpoch(1)
		if err := stream.Send(ready); err != nil {
			return err
		}
	case first.GetResume() != nil:
		resumed := new(sessionv1.ConnectResponse)
		resumed.SetOpcode(opDispatch)
		resumed.SetSequence(100)
		resumed.SetType(eventResumed)
		resumed.SetJsonPayload(`{"session_id":"` + first.GetResume().GetSessionId() + `"}`)
		resumed.SetSessionId(first.GetResume().GetSessionId())
		resumed.SetBindingEpoch(2)
		if err := stream.Send(resumed); err != nil {
			return err
		}
	default:
		return fmt.Errorf("identify or resume is required")
	}
	for {
		frame, err := stream.Recv()
		if err != nil {
			return err
		}
		switch {
		case frame.GetHeartbeat() != nil:
			return fmt.Errorf("gateway forwarded heartbeat to session")
		case frame.GetSubscribe() != nil:
			subscribed := new(sessionv1.ConnectResponse)
			subscribed.SetOpcode(opDispatch)
			subscribed.SetType(eventSubscribed)
			subscribed.SetJsonPayload(`{"channel_ids":["42"]}`)
			if err := stream.Send(subscribed); err != nil {
				return err
			}
		case frame.GetPresence() != nil:
		case frame.GetDetach() != nil:
			return nil
		}
	}
}

func startFakeSessionServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	sessionv1.RegisterSessionServiceServer(server, fakeSessionServer{})
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(server.Stop)
	return listener.Addr().String()
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

	writeClientText(t, conn, `{"op":6,"d":{"token":"access-token","session_id":"sess-1","seq":42}}`)
	resumed := readEnvelope(t, reader)
	require.Equal(t, opDispatch, resumed.Op)
	require.Equal(t, eventResumed, resumed.T)
	require.Equal(t, uint64(100), resumed.S)
}

func TestWebSocketSubscribeLifecycle(t *testing.T) {
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

	writeClientText(t, conn, `{"op":4,"d":{"channel_ids":["42"]}}`)
	subscribed := readEnvelope(t, reader)
	require.Equal(t, opDispatch, subscribed.Op)
	require.Equal(t, eventSubscribed, subscribed.T)
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

	writeClientText(t, conn, `{"op":3,"d":{"status":"online","client_state":"desktop"}}`)
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

type fakeResolver struct {
	address string
	err     error
}

func (f fakeResolver) ResolveNode(context.Context) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.address, nil
}

func (f fakeResolver) ResolveSession(context.Context, string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.address, nil
}

var _ discovery.Resolver = fakeResolver{}

func connectWebSocket(t *testing.T, gateway *Server, path string) (net.Conn, *bufio.Reader) {
	t.Helper()
	var keyBytes [16]byte
	_, err := rand.Read(keyBytes[:])
	require.NoError(t, err)
	key := base64.StdEncoding.EncodeToString(keyBytes[:])

	serverConn, conn := net.Pipe()
	response := &hijackResponse{
		header: http.Header{},
		conn:   serverConn,
		rw:     bufio.NewReadWriter(bufio.NewReader(serverConn), bufio.NewWriter(serverConn)),
	}
	req, err := http.NewRequest(http.MethodGet, "http://gateway.test"+path, nil)
	require.NoError(t, err)
	req.RemoteAddr = "127.0.0.1:43210"
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", key)

	go gateway.Handler().ServeHTTP(response, req)
	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Contains(t, statusLine, "101")
	for {
		line, err := reader.ReadString('\n')
		require.NoError(t, err)
		if strings.TrimSpace(line) == "" {
			break
		}
	}
	return conn, reader
}

type hijackResponse struct {
	header http.Header
	conn   net.Conn
	rw     *bufio.ReadWriter
	wrote  bool
}

func (r *hijackResponse) Header() http.Header               { return r.header }
func (r *hijackResponse) Write(payload []byte) (int, error) { return r.conn.Write(payload) }
func (r *hijackResponse) WriteHeader(code int) {
	if r.wrote {
		return
	}
	r.wrote = true
	_, _ = fmt.Fprintf(r.rw, "HTTP/1.1 %d %s\r\n", code, http.StatusText(code))
	for key, values := range r.header {
		for _, value := range values {
			_, _ = r.rw.WriteString(key + ": " + value + "\r\n")
		}
	}
	_, _ = r.rw.WriteString("\r\n")
	_ = r.rw.Flush()
}
func (r *hijackResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return r.conn, r.rw, nil
}

func readEnvelope(t *testing.T, reader *bufio.Reader) envelope {
	t.Helper()
	payload := readServerText(t, reader)
	var msg envelope
	require.NoError(t, json.Unmarshal(payload, &msg))
	return msg
}

func readServerText(t *testing.T, reader *bufio.Reader) []byte {
	t.Helper()
	header := make([]byte, 2)
	_, err := io.ReadFull(reader, header)
	require.NoError(t, err)
	require.Equal(t, byte(0x81), header[0])
	size := int(header[1] & 0x7f)
	switch size {
	case 126:
		ext := make([]byte, 2)
		_, err = io.ReadFull(reader, ext)
		require.NoError(t, err)
		size = int(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		_, err = io.ReadFull(reader, ext)
		require.NoError(t, err)
		size = int(binary.BigEndian.Uint64(ext))
	}
	payload := make([]byte, size)
	_, err = io.ReadFull(reader, payload)
	require.NoError(t, err)
	return payload
}

func writeClientText(t *testing.T, conn net.Conn, payload string) {
	t.Helper()
	_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
	mask := []byte{1, 2, 3, 4}
	body := []byte(payload)
	header := []byte{0x81}
	if len(body) < 126 {
		header = append(header, 0x80|byte(len(body)))
	} else {
		header = append(header, 0x80|126, byte(len(body)>>8), byte(len(body)))
	}
	frame := append(header, mask...)
	for i, value := range body {
		frame = append(frame, value^mask[i%4])
	}
	_, err := conn.Write(frame)
	require.NoError(t, err)
}
