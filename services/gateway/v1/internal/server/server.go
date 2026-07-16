package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	"github.com/soasurs/cordis/services/gateway/v1/internal/svc"
)

type Server struct {
	svcCtx     *svc.ServiceContext
	gatewayID  string
	generation string
	rpcAddr    string
	hub        *hub
}

type client struct {
	server           *Server
	ws               *websocket.Conn
	userID           int64
	authSessionID    int64
	gatewaySessionID string
	deviceType       string
	status           presencev1.PresenceStatus
	clientState      presencev1.ClientState
	channels         map[int64]struct{}
	send             chan envelope
	done             chan struct{}
	closeOnce        sync.Once
}

func New(svcCtx *svc.ServiceContext) *Server {
	if svcCtx.Cfg.RPC.ListenOn == "" {
		panic("gateway rpc listen address is required")
	}
	return &Server{
		svcCtx:     svcCtx,
		gatewayID:  randomID("gw"),
		generation: randomID("gen"),
		rpcAddr:    advertiseAddr(svcCtx.Cfg.RPC.ListenOn),
		hub:        newHub(),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc(s.svcCtx.Cfg.Gateway.WebSocketRoute(), s.handleWebSocket)
	return mux
}

func (s *Server) StartBackground(ctx context.Context) {
	go s.refreshGateway(ctx)
	go s.refreshRoutes(ctx)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	ws.SetReadLimit(s.svcCtx.Cfg.Gateway.MessageLimit())

	c := &client{
		server:           s,
		ws:               ws,
		gatewaySessionID: randomID("sess"),
		status:           presencev1.PresenceStatus_PRESENCE_STATUS_ONLINE,
		clientState:      presencev1.ClientState_CLIENT_STATE_FOREGROUND,
		channels:         make(map[int64]struct{}),
		send:             make(chan envelope, 64),
		done:             make(chan struct{}),
	}
	c.run(context.Background())
}

func (c *client) run(ctx context.Context) {
	defer c.close(ctx)
	go c.writeLoop(ctx)

	if err := c.write(opHello, "HELLO", helloData{
		HeartbeatIntervalMs: c.server.svcCtx.Cfg.Gateway.HeartbeatInterval().Milliseconds(),
		GatewayID:           c.server.gatewayID,
	}); err != nil {
		return
	}
	if err := c.identify(ctx); err != nil {
		_ = c.writeDirect(ctx, opError, "ERROR", errorData{Code: "identify_failed", Message: err.Error()})
		_ = c.ws.Close(websocket.StatusPolicyViolation, "identify failed")
		return
	}
	c.readLoop(ctx)
}

func (c *client) identify(ctx context.Context) error {
	readCtx, cancel := context.WithTimeout(ctx, c.server.svcCtx.Cfg.Gateway.IdentifyTimeout())
	defer cancel()

	var msg envelope
	if err := wsjson.Read(readCtx, c.ws, &msg); err != nil {
		return err
	}
	if msg.Op != opIdentify {
		return errors.New("first websocket message must be IDENTIFY")
	}
	var data identifyData
	if err := json.Unmarshal(msg.D, &data); err != nil {
		return err
	}
	if strings.TrimSpace(data.Token) == "" {
		return errors.New("token is required")
	}

	req := new(authenticatorv1.VerifyAccessTokenRequest)
	req.SetAccessToken(data.Token)
	auth, err := c.server.svcCtx.AuthenticatorClient.VerifyAccessToken(ctx, req)
	if err != nil {
		return err
	}
	if !auth.GetOk() || auth.GetUserId() == 0 || auth.GetSessionId() == 0 {
		return errors.New("access token rejected")
	}

	c.userID = auth.GetUserId()
	c.authSessionID = auth.GetSessionId()
	c.deviceType = data.DeviceType
	c.status = statusFromString(data.Status)
	c.clientState = clientStateFromString(data.ClientState)

	registerReq := new(presencev1.RegisterUserSessionRequest)
	registerReq.SetUserId(c.userID)
	registerReq.SetSessionId(c.gatewaySessionID)
	registerReq.SetGatewayId(c.server.gatewayID)
	registerReq.SetGeneration(c.server.generation)
	registerReq.SetDeviceType(c.deviceType)
	registerReq.SetStatus(c.status)
	registerReq.SetClientState(c.clientState)
	if _, err := c.server.svcCtx.PresenceClient.RegisterUserSession(ctx, registerReq); err != nil {
		return err
	}
	c.server.hub.add(c)

	return c.write(opDispatch, "READY", readyData{
		UserID:               c.userID,
		AuthSessionID:        c.authSessionID,
		GatewaySessionID:     c.gatewaySessionID,
		GatewayID:            c.server.gatewayID,
		HeartbeatIntervalMs:  c.server.svcCtx.Cfg.Gateway.HeartbeatInterval().Milliseconds(),
		AccessTokenExpiresAt: auth.GetExpiresAt(),
	})
}

func (c *client) readLoop(ctx context.Context) {
	for {
		var msg envelope
		err := wsjson.Read(ctx, c.ws, &msg)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusMessageTooBig {
				_ = c.ws.Close(websocket.StatusMessageTooBig, "message too large")
			}
			return
		}
		if err := c.handle(ctx, msg); err != nil {
			logx.WithContext(ctx).Errorw("handle websocket message",
				logx.Field("user_id", c.userID),
				logx.Field("session_id", c.gatewaySessionID),
				logx.Field("error", err),
			)
			_ = c.write(opError, "ERROR", errorData{Code: "operation_failed", Message: err.Error()})
		}
	}
}

func (c *client) handle(ctx context.Context, msg envelope) error {
	switch msg.Op {
	case opHeartbeat:
		return c.refreshPresence(ctx)
	case opPresence:
		var data presenceData
		if err := json.Unmarshal(msg.D, &data); err != nil {
			return err
		}
		c.status = statusFromString(data.Status)
		c.clientState = clientStateFromString(data.ClientState)
		req := new(presencev1.UpdateUserPresenceRequest)
		req.SetUserId(c.userID)
		req.SetSessionId(c.gatewaySessionID)
		req.SetStatus(c.status)
		req.SetClientState(c.clientState)
		if _, err := c.server.svcCtx.PresenceClient.UpdateUserPresence(ctx, req); err != nil {
			return err
		}
		return nil
	case opSubscribe:
		var data subscribeData
		if err := json.Unmarshal(msg.D, &data); err != nil {
			return err
		}
		channelIDs, err := c.server.authorizeChannelSubscriptions(ctx, c.userID, data.ChannelIDs)
		if err != nil {
			return err
		}
		newlyActive := c.server.hub.subscribe(c, channelIDs)
		if len(newlyActive) > 0 {
			req := new(presencev1.RefreshChannelRoutesRequest)
			req.SetGatewayId(c.server.gatewayID)
			req.SetGeneration(c.server.generation)
			req.SetChannelIds(newlyActive)
			if _, err := c.server.svcCtx.PresenceClient.RefreshChannelRoutes(ctx, req); err != nil {
				return err
			}
		}
		return c.write(opDispatch, "SUBSCRIBED", subscribedData{ChannelIDs: channelIDs})
	default:
		return errors.New("unsupported gateway op")
	}
}

func (c *client) refreshPresence(ctx context.Context) error {
	req := new(presencev1.RefreshUserSessionRequest)
	req.SetUserId(c.userID)
	req.SetSessionId(c.gatewaySessionID)
	req.SetGatewayId(c.server.gatewayID)
	req.SetGeneration(c.server.generation)
	req.SetDeviceType(c.deviceType)
	req.SetStatus(c.status)
	req.SetClientState(c.clientState)
	if _, err := c.server.svcCtx.PresenceClient.RefreshUserSession(ctx, req); err != nil {
		return err
	}
	return c.write(opHeartbeatAck, "HEARTBEAT_ACK", heartbeatAckData{
		UserID:           c.userID,
		GatewaySessionID: c.gatewaySessionID,
	})
}

func (c *client) close(ctx context.Context) {
	c.closeOnce.Do(func() {
		close(c.done)
	})
	inactive := c.server.hub.remove(c)
	for _, channelID := range inactive {
		req := new(presencev1.DetachChannelRouteRequest)
		req.SetGatewayId(c.server.gatewayID)
		req.SetGeneration(c.server.generation)
		req.SetChannelId(channelID)
		if _, err := c.server.svcCtx.PresenceClient.DetachChannelRoute(ctx, req); err != nil {
			logx.WithContext(ctx).Errorw("detach channel route",
				logx.Field("channel_id", channelID),
				logx.Field("error", err),
			)
		}
	}
	if c.userID != 0 && c.gatewaySessionID != "" {
		req := new(presencev1.RemoveUserSessionRequest)
		req.SetUserId(c.userID)
		req.SetSessionId(c.gatewaySessionID)
		if _, err := c.server.svcCtx.PresenceClient.RemoveUserSession(ctx, req); err != nil {
			logx.WithContext(ctx).Errorw("remove user session",
				logx.Field("user_id", c.userID),
				logx.Field("session_id", c.gatewaySessionID),
				logx.Field("error", err),
			)
		}
	}
	_ = c.ws.Close(websocket.StatusNormalClosure, "")
}

func (c *client) write(op int, event string, data any) error {
	msg := makeEnvelope(op, event, data)
	return c.enqueue(msg)
}

func (c *client) dispatch(event string, payload json.RawMessage) error {
	msg := envelope{Op: opDispatch, T: event, D: payload}
	return c.enqueue(msg)
}

func (c *client) enqueue(msg envelope) error {
	select {
	case c.send <- msg:
		return nil
	case <-c.done:
		return errors.New("websocket client closed")
	default:
		return errors.New("websocket client send queue full")
	}
}

func (c *client) writeLoop(ctx context.Context) {
	for {
		select {
		case msg := <-c.send:
			if err := wsjson.Write(context.Background(), c.ws, msg); err != nil {
				logx.WithContext(ctx).Errorw("write websocket message",
					logx.Field("user_id", c.userID),
					logx.Field("session_id", c.gatewaySessionID),
					logx.Field("error", err),
				)
				_ = c.ws.Close(websocket.StatusInternalError, "write failed")
				return
			}
		case <-c.done:
			return
		}
	}
}

func (c *client) writeDirect(ctx context.Context, op int, event string, data any) error {
	return wsjson.Write(ctx, c.ws, makeEnvelope(op, event, data))
}

func validateDispatchEvent(eventType, payload string) (json.RawMessage, error) {
	if strings.TrimSpace(eventType) == "" {
		return nil, status.Error(codes.InvalidArgument, "event type is required")
	}
	if strings.TrimSpace(payload) == "" {
		return json.RawMessage(`{}`), nil
	}
	raw := json.RawMessage(payload)
	if !json.Valid(raw) {
		return nil, status.Error(codes.InvalidArgument, "json payload is invalid")
	}
	return raw, nil
}

func (s *Server) refreshGateway(ctx context.Context) {
	ticker := time.NewTicker(s.svcCtx.Cfg.Gateway.GatewayRefreshInterval())
	defer ticker.Stop()
	for {
		if err := s.registerGateway(ctx); err != nil {
			logx.WithContext(ctx).Errorw("register gateway",
				logx.Field("gateway_id", s.gatewayID),
				logx.Field("error", err),
			)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) registerGateway(ctx context.Context) error {
	req := new(presencev1.RegisterGatewayRequest)
	req.SetGatewayId(s.gatewayID)
	req.SetGeneration(s.generation)
	req.SetRpcAddr(s.rpcAddr)
	_, err := s.svcCtx.PresenceClient.RegisterGateway(ctx, req)
	return err
}

func (s *Server) refreshRoutes(ctx context.Context) {
	ticker := time.NewTicker(s.svcCtx.Cfg.Gateway.RouteRefreshInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s.revalidateChannelSubscriptions(ctx)
		channels := s.hub.activeChannels()
		if len(channels) == 0 {
			continue
		}
		req := new(presencev1.RefreshChannelRoutesRequest)
		req.SetGatewayId(s.gatewayID)
		req.SetGeneration(s.generation)
		req.SetChannelIds(channels)
		if _, err := s.svcCtx.PresenceClient.RefreshChannelRoutes(ctx, req); err != nil {
			logx.WithContext(ctx).Errorw("refresh channel routes",
				logx.Field("channel_count", len(channels)),
				logx.Field("error", err),
			)
		}
	}
}

func (s *Server) revalidateChannelSubscriptions(ctx context.Context) {
	for _, subscription := range s.hub.channelSubscriptions() {
		allowed, err := s.authorizeChannelSubscription(ctx, subscription.client.userID, subscription.channelID)
		if err != nil && !subscriptionInvalid(err) {
			// Transient Guild failures must not evict a valid subscription.
			// The next route refresh retries authorization.
			logx.WithContext(ctx).Errorw("revalidate channel subscription",
				logx.Field("user_id", subscription.client.userID),
				logx.Field("channel_id", subscription.channelID),
				logx.Field("error", err),
			)
			continue
		}
		if allowed && err == nil {
			continue
		}
		if s.hub.unsubscribe(subscription.client, subscription.channelID) {
			req := new(presencev1.DetachChannelRouteRequest)
			req.SetGatewayId(s.gatewayID)
			req.SetGeneration(s.generation)
			req.SetChannelId(subscription.channelID)
			if _, detachErr := s.svcCtx.PresenceClient.DetachChannelRoute(ctx, req); detachErr != nil {
				logx.WithContext(ctx).Errorw("detach unauthorized channel route",
					logx.Field("channel_id", subscription.channelID),
					logx.Field("error", detachErr),
				)
			}
		}
	}
}

func statusFromString(status string) presencev1.PresenceStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "idle":
		return presencev1.PresenceStatus_PRESENCE_STATUS_IDLE
	case "dnd":
		return presencev1.PresenceStatus_PRESENCE_STATUS_DND
	case "invisible":
		return presencev1.PresenceStatus_PRESENCE_STATUS_INVISIBLE
	default:
		return presencev1.PresenceStatus_PRESENCE_STATUS_ONLINE
	}
}

func clientStateFromString(state string) presencev1.ClientState {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "background":
		return presencev1.ClientState_CLIENT_STATE_BACKGROUND
	default:
		return presencev1.ClientState_CLIENT_STATE_FOREGROUND
	}
}

func randomID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + "-" + time.Now().Format("20060102150405.000000000")
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

func advertiseAddr(listenOn string) string {
	host, port, err := net.SplitHostPort(listenOn)
	if err != nil {
		return listenOn
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		hostname, err := os.Hostname()
		if err == nil && hostname != "" {
			host = hostname
		}
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}
