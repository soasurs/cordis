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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	gatewayv1 "github.com/soasurs/cordis/gen/gateway/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	"github.com/soasurs/cordis/services/gateway/v1/config"
	"github.com/soasurs/cordis/services/gateway/v1/internal/svc"
)

func TestWebSocketIdentifyHeartbeatAndSubscribe(t *testing.T) {
	auth := &fakeAuthenticatorClient{userID: 1001, sessionID: 2002, expiresAt: 3003}
	presence := &fakePresenceClient{}
	gateway := newTestGateway(auth, presence)

	conn, reader := connectWebSocket(t, gateway, "/ws")
	defer conn.Close()

	hello := readEnvelope(t, reader)
	require.Equal(t, opHello, hello.Op)
	require.Equal(t, "HELLO", hello.T)

	writeClientText(t, conn, `{"op":2,"d":{"token":"access-token","device_type":"desktop","status":"dnd","client_state":"background"}}`)
	ready := readEnvelope(t, reader)
	require.Equal(t, opDispatch, ready.Op)
	require.Equal(t, "READY", ready.T)

	presence.mu.Lock()
	require.Equal(t, "access-token", auth.verifiedToken)
	require.Equal(t, int64(1001), presence.registered.GetUserId())
	require.Equal(t, gateway.gatewayID, presence.registered.GetGatewayId())
	require.Equal(t, gateway.generation, presence.registered.GetGeneration())
	require.NotEmpty(t, presence.registered.GetGatewayId())
	require.NotEmpty(t, presence.registered.GetGeneration())
	require.Equal(t, presencev1.PresenceStatus_PRESENCE_STATUS_DND, presence.registered.GetStatus())
	require.Equal(t, presencev1.ClientState_CLIENT_STATE_BACKGROUND, presence.registered.GetClientState())
	gatewaySessionID := presence.registered.GetSessionId()
	presence.mu.Unlock()
	require.NotEmpty(t, gatewaySessionID)

	writeClientText(t, conn, `{"op":1}`)
	ack := readEnvelope(t, reader)
	require.Equal(t, opHeartbeatAck, ack.Op)
	require.Equal(t, "HEARTBEAT_ACK", ack.T)

	presence.mu.Lock()
	require.Equal(t, gatewaySessionID, presence.refreshed.GetSessionId())
	require.Equal(t, int64(1001), presence.refreshed.GetUserId())
	presence.mu.Unlock()

	writeClientText(t, conn, `{"op":4,"d":{"channel_ids":[7001,7002]}}`)
	subscribed := readEnvelope(t, reader)
	require.Equal(t, "SUBSCRIBED", subscribed.T)

	presence.mu.Lock()
	require.Equal(t, []int64{7001, 7002}, presence.refreshedChannels)
	presence.mu.Unlock()
}

func TestWebSocketRejectsMissingIdentify(t *testing.T) {
	auth := &fakeAuthenticatorClient{userID: 1001, sessionID: 2002}
	presence := &fakePresenceClient{}
	gateway := newTestGateway(auth, presence)

	conn, reader := connectWebSocket(t, gateway, "/ws")
	defer conn.Close()
	_ = readEnvelope(t, reader)

	writeClientText(t, conn, `{"op":1}`)
	msg := readEnvelope(t, reader)
	require.Equal(t, opError, msg.Op)
	require.Equal(t, "ERROR", msg.T)
	require.Empty(t, auth.verifiedToken)
	require.Nil(t, presence.registered)
}

func TestWebSocketRejectsUnauthorizedSubscription(t *testing.T) {
	auth := &fakeAuthenticatorClient{userID: 1001, sessionID: 2002}
	presence := &fakePresenceClient{}
	guild := &fakeGuildClient{deniedChannels: map[int64]bool{7002: true}}
	gateway := newTestGatewayWithGuild(auth, presence, guild)

	conn, reader := connectWebSocket(t, gateway, "/ws")
	defer conn.Close()
	_ = readEnvelope(t, reader)
	writeClientText(t, conn, `{"op":2,"d":{"token":"access-token"}}`)
	_ = readEnvelope(t, reader)
	writeClientText(t, conn, `{"op":4,"d":{"channel_ids":[7001,7002]}}`)
	failure := readEnvelope(t, reader)
	require.Equal(t, opError, failure.Op)
	require.Equal(t, "ERROR", failure.T)
	require.Empty(t, gateway.hub.activeChannels())
}

func TestDispatchRPCDeliversToWebSocketClients(t *testing.T) {
	auth := &fakeAuthenticatorClient{userID: 1001, sessionID: 2002}
	presence := &fakePresenceClient{}
	gateway := newTestGateway(auth, presence)

	conn, reader := connectWebSocket(t, gateway, "/ws")
	defer conn.Close()
	_ = readEnvelope(t, reader)
	writeClientText(t, conn, `{"op":2,"d":{"token":"access-token"}}`)
	_ = readEnvelope(t, reader)
	writeClientText(t, conn, `{"op":4,"d":{"channel_ids":[7001]}}`)
	_ = readEnvelope(t, reader)

	channelReq := new(gatewayv1.DispatchChannelEventRequest)
	channelReq.SetChannelId(7001)
	channelReq.SetEvent(dispatchEvent("message_created", `{"id":"m1"}`))
	channelResp, err := gateway.DispatchChannelEvent(t.Context(), channelReq)
	require.NoError(t, err)
	require.Equal(t, int32(1), channelResp.GetDelivered())
	requireDispatch(t, readEnvelope(t, reader), "message_created", `{"id":"m1"}`)

	userReq := new(gatewayv1.DispatchUserEventRequest)
	userReq.SetUserId(1001)
	userReq.SetEvent(dispatchEvent("presence_updated", `{"user_id":1001}`))
	userResp, err := gateway.DispatchUserEvent(t.Context(), userReq)
	require.NoError(t, err)
	require.Equal(t, int32(1), userResp.GetDelivered())
	requireDispatch(t, readEnvelope(t, reader), "presence_updated", `{"user_id":1001}`)

	presence.mu.Lock()
	sessionID := presence.registered.GetSessionId()
	presence.mu.Unlock()
	sessionReq := new(gatewayv1.DispatchSessionEventRequest)
	sessionReq.SetSessionId(sessionID)
	sessionReq.SetEvent(dispatchEvent("session_notice", `{"ok":true}`))
	sessionResp, err := gateway.DispatchSessionEvent(t.Context(), sessionReq)
	require.NoError(t, err)
	require.Equal(t, int32(1), sessionResp.GetDelivered())
	requireDispatch(t, readEnvelope(t, reader), "session_notice", `{"ok":true}`)
}

func TestDispatchRPCValidation(t *testing.T) {
	gateway := newTestGateway(&fakeAuthenticatorClient{}, &fakePresenceClient{})

	req := new(gatewayv1.DispatchChannelEventRequest)
	req.SetChannelId(7001)
	req.SetEvent(dispatchEvent("", `{}`))
	_, err := gateway.DispatchChannelEvent(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	req.SetEvent(dispatchEvent("bad_payload", `{`))
	_, err = gateway.DispatchChannelEvent(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestBackgroundRegistersGatewayAndRefreshesRoutes(t *testing.T) {
	presence := &fakePresenceClient{}
	gateway := newTestGateway(&fakeAuthenticatorClient{}, presence)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	gateway.hub.subscribe(&client{channels: make(map[int64]struct{})}, []int64{9001})
	gateway.StartBackground(ctx)

	require.Eventually(t, func() bool {
		presence.mu.Lock()
		defer presence.mu.Unlock()
		return presence.gateway != nil && len(presence.refreshedChannels) == 1 && presence.refreshedChannels[0] == 9001
	}, 2*time.Second, 20*time.Millisecond)
}

func TestRouteRefreshRemovesUnauthorizedSubscription(t *testing.T) {
	presence := &fakePresenceClient{}
	guild := &fakeGuildClient{}
	gateway := newTestGatewayWithGuild(&fakeAuthenticatorClient{}, presence, guild)
	c := &client{userID: 1001, channels: make(map[int64]struct{})}
	gateway.hub.subscribe(c, []int64{9001})
	guild.deniedChannels = map[int64]bool{9001: true}

	gateway.revalidateChannelSubscriptions(t.Context())
	require.Empty(t, gateway.hub.activeChannels())
}

func TestRouteRefreshKeepsSubscriptionOnTransientGuildFailure(t *testing.T) {
	presence := &fakePresenceClient{}
	guild := &fakeGuildClient{err: status.Error(codes.Unavailable, "guild unavailable")}
	gateway := newTestGatewayWithGuild(&fakeAuthenticatorClient{}, presence, guild)
	c := &client{userID: 1001, channels: make(map[int64]struct{})}
	gateway.hub.subscribe(c, []int64{9001})

	gateway.revalidateChannelSubscriptions(t.Context())
	require.Equal(t, []int64{9001}, gateway.hub.activeChannels())
}

func TestNormalizeChannelIDsDeduplicatesAndRejectsInvalid(t *testing.T) {
	channelIDs, err := normalizeChannelIDs([]int64{7001, 7001, 7002})
	require.NoError(t, err)
	require.Equal(t, []int64{7001, 7002}, channelIDs)

	_, err = normalizeChannelIDs([]int64{0})
	require.Error(t, err)
}

func newTestGateway(auth authenticatorv1.AuthenticatorServiceClient, presence presencev1.PresenceServiceClient) *Server {
	return newTestGatewayWithGuild(auth, presence, &fakeGuildClient{})
}

func newTestGatewayWithGuild(
	auth authenticatorv1.AuthenticatorServiceClient,
	presence presencev1.PresenceServiceClient,
	guild guildv1.GuildServiceClient,
) *Server {
	cfg := config.Config{
		Name:     "gateway.test",
		ListenOn: "127.0.0.1:8081",
		RPC: zrpc.RpcServerConf{
			ListenOn: "127.0.0.1:3004",
		},
		Gateway: config.GatewayConfig{
			WebSocketPath:          "/ws",
			HeartbeatIntervalMs:    50,
			IdentifyTimeoutSeconds: 1,
			RouteRefreshSeconds:    1,
			GatewayRefreshSeconds:  1,
		},
	}
	svcCtx := svc.NewServiceContextWithDependencies(cfg, svc.Dependencies{
		AuthenticatorClient: auth,
		PresenceClient:      presence,
		GuildClient:         guild,
	})
	return New(svcCtx)
}

type fakeGuildClient struct {
	guildv1.GuildServiceClient
	mu             sync.Mutex
	deniedChannels map[int64]bool
	err            error
}

func (f *fakeGuildClient) ListUserGuilds(
	context.Context,
	*guildv1.ListUserGuildsRequest,
	...grpc.CallOption,
) (*guildv1.ListUserGuildsResponse, error) {
	return new(guildv1.ListUserGuildsResponse), nil
}

func (f *fakeGuildClient) AuthorizeGuildChannel(
	_ context.Context,
	req *guildv1.AuthorizeGuildChannelRequest,
	_ ...grpc.CallOption,
) (*guildv1.AuthorizeGuildChannelResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	resp := new(guildv1.AuthorizeGuildChannelResponse)
	resp.SetAllowed(!f.deniedChannels[req.GetChannelId()])
	resp.SetPermissions(permissionViewChannel)
	return resp, nil
}

func TestAdvertiseAddr(t *testing.T) {
	require.Equal(t, "127.0.0.1:8081", advertiseAddr("127.0.0.1:8081"))
	require.NotEqual(t, "0.0.0.0:8081", advertiseAddr("0.0.0.0:8081"))
	require.NotEmpty(t, advertiseAddr(":8081"))
}

func dispatchEvent(eventType, payload string) *gatewayv1.EventEnvelope {
	event := new(gatewayv1.EventEnvelope)
	event.SetType(eventType)
	event.SetJsonPayload(payload)
	return event
}

func requireDispatch(t *testing.T, msg envelope, eventType, payload string) {
	t.Helper()

	require.Equal(t, opDispatch, msg.Op)
	require.Equal(t, eventType, msg.T)
	require.JSONEq(t, payload, string(msg.D))
}

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
	status, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Contains(t, status, "101")
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

func (r *hijackResponse) Header() http.Header {
	return r.header
}

func (r *hijackResponse) Write(payload []byte) (int, error) {
	return r.conn.Write(payload)
}

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
	mask := []byte{1, 2, 3, 4}
	body := []byte(payload)
	header := []byte{0x81}
	if len(body) < 126 {
		header = append(header, 0x80|byte(len(body)))
	} else {
		header = append(header, 0x80|126, byte(len(body)>>8), byte(len(body)))
	}
	frame := append(header, mask...)
	for i, b := range body {
		frame = append(frame, b^mask[i%4])
	}
	_, err := conn.Write(frame)
	require.NoError(t, err)
}

type fakeAuthenticatorClient struct {
	verifiedToken string
	userID        int64
	sessionID     int64
	expiresAt     int64
}

func (f *fakeAuthenticatorClient) Register(context.Context, *authenticatorv1.RegisterRequest, ...grpc.CallOption) (*authenticatorv1.RegisterResponse, error) {
	return nil, nil
}

func (f *fakeAuthenticatorClient) Login(context.Context, *authenticatorv1.LoginRequest, ...grpc.CallOption) (*authenticatorv1.LoginResponse, error) {
	return nil, nil
}

func (f *fakeAuthenticatorClient) Refresh(context.Context, *authenticatorv1.RefreshRequest, ...grpc.CallOption) (*authenticatorv1.RefreshResponse, error) {
	return nil, nil
}

func (f *fakeAuthenticatorClient) Logout(context.Context, *authenticatorv1.LogoutRequest, ...grpc.CallOption) (*authenticatorv1.LogoutResponse, error) {
	return nil, nil
}

func (f *fakeAuthenticatorClient) VerifyAccessToken(_ context.Context, req *authenticatorv1.VerifyAccessTokenRequest, _ ...grpc.CallOption) (*authenticatorv1.VerifyAccessTokenResponse, error) {
	f.verifiedToken = req.GetAccessToken()
	resp := new(authenticatorv1.VerifyAccessTokenResponse)
	resp.SetOk(f.userID != 0 && f.sessionID != 0)
	resp.SetUserId(f.userID)
	resp.SetSessionId(f.sessionID)
	resp.SetExpiresAt(f.expiresAt)
	return resp, nil
}

func (f *fakeAuthenticatorClient) ListSessions(context.Context, *authenticatorv1.ListSessionsRequest, ...grpc.CallOption) (*authenticatorv1.ListSessionsResponse, error) {
	return nil, nil
}

func (f *fakeAuthenticatorClient) RevokeUserSession(context.Context, *authenticatorv1.RevokeUserSessionRequest, ...grpc.CallOption) (*authenticatorv1.RevokeUserSessionResponse, error) {
	return nil, nil
}

func (f *fakeAuthenticatorClient) RevokeOtherSessions(context.Context, *authenticatorv1.RevokeOtherSessionsRequest, ...grpc.CallOption) (*authenticatorv1.RevokeOtherSessionsResponse, error) {
	return nil, nil
}

type fakePresenceClient struct {
	mu sync.Mutex

	gateway           *presencev1.RegisterGatewayRequest
	registered        *presencev1.RegisterUserSessionRequest
	refreshed         *presencev1.RefreshUserSessionRequest
	refreshedChannels []int64
}

func (f *fakePresenceClient) RegisterGateway(_ context.Context, req *presencev1.RegisterGatewayRequest, _ ...grpc.CallOption) (*presencev1.RegisterGatewayResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gateway = req
	return new(presencev1.RegisterGatewayResponse), nil
}

func (f *fakePresenceClient) RefreshChannelRoutes(_ context.Context, req *presencev1.RefreshChannelRoutesRequest, _ ...grpc.CallOption) (*presencev1.RefreshChannelRoutesResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshedChannels = append([]int64(nil), req.GetChannelIds()...)
	resp := new(presencev1.RefreshChannelRoutesResponse)
	resp.SetRefreshed(int32(len(req.GetChannelIds())))
	return resp, nil
}

func (f *fakePresenceClient) DetachChannelRoute(context.Context, *presencev1.DetachChannelRouteRequest, ...grpc.CallOption) (*presencev1.DetachChannelRouteResponse, error) {
	return new(presencev1.DetachChannelRouteResponse), nil
}

func (f *fakePresenceClient) ResolveChannelGateways(context.Context, *presencev1.ResolveChannelGatewaysRequest, ...grpc.CallOption) (*presencev1.ResolveChannelGatewaysResponse, error) {
	return new(presencev1.ResolveChannelGatewaysResponse), nil
}

func (f *fakePresenceClient) RegisterUserSession(_ context.Context, req *presencev1.RegisterUserSessionRequest, _ ...grpc.CallOption) (*presencev1.RegisterUserSessionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registered = req
	return new(presencev1.RegisterUserSessionResponse), nil
}

func (f *fakePresenceClient) RefreshUserSession(_ context.Context, req *presencev1.RefreshUserSessionRequest, _ ...grpc.CallOption) (*presencev1.RefreshUserSessionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshed = req
	return new(presencev1.RefreshUserSessionResponse), nil
}

func (f *fakePresenceClient) UpdateUserPresence(context.Context, *presencev1.UpdateUserPresenceRequest, ...grpc.CallOption) (*presencev1.UpdateUserPresenceResponse, error) {
	return new(presencev1.UpdateUserPresenceResponse), nil
}

func (f *fakePresenceClient) RemoveUserSession(context.Context, *presencev1.RemoveUserSessionRequest, ...grpc.CallOption) (*presencev1.RemoveUserSessionResponse, error) {
	return new(presencev1.RemoveUserSessionResponse), nil
}

func (f *fakePresenceClient) ResolveUsersPresence(context.Context, *presencev1.ResolveUsersPresenceRequest, ...grpc.CallOption) (*presencev1.ResolveUsersPresenceResponse, error) {
	return new(presencev1.ResolveUsersPresenceResponse), nil
}
