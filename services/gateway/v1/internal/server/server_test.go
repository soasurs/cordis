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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/services/gateway/v1/config"
	"github.com/soasurs/cordis/services/gateway/v1/internal/discovery"
	"github.com/soasurs/cordis/services/gateway/v1/internal/svc"
)

func TestWebSocketForwardsSessionFrames(t *testing.T) {
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

type fakeSessionServer struct {
	sessionv1.UnimplementedSessionServiceServer
}

func (fakeSessionServer) Connect(stream sessionv1.SessionService_ConnectServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	if first.GetIdentify() == nil {
		return fmt.Errorf("identify is required")
	}
	ready := new(sessionv1.ConnectResponse)
	ready.SetOpcode(opDispatch)
	ready.SetSequence(1)
	ready.SetType(eventReady)
	ready.SetJsonPayload(`{"session_id":"sess-test"}`)
	if err := stream.Send(ready); err != nil {
		return err
	}
	for {
		frame, err := stream.Recv()
		if err != nil {
			return err
		}
		if frame.GetHeartbeat() != nil {
			ack := new(sessionv1.ConnectResponse)
			ack.SetOpcode(opHeartbeatAck)
			ack.SetJsonPayload(`null`)
			if err := stream.Send(ack); err != nil {
				return err
			}
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

type fakeResolver struct {
	address string
}

func (f fakeResolver) ResolveNode(context.Context) (string, error) {
	return f.address, nil
}

func (f fakeResolver) ResolveSession(context.Context, string) (string, error) {
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
