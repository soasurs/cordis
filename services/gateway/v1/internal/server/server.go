package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/clientip"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/pkg/socketlimit"
	"github.com/soasurs/cordis/services/gateway/v1/internal/svc"
	gatewayratelimit "github.com/soasurs/cordis/services/gateway/v1/ratelimit"
)

const (
	eventHello        = realtime.GatewayEventHello
	eventReady        = realtime.GatewayEventReady
	eventResumed      = realtime.GatewayEventResumed
	eventHeartbeatAck = realtime.GatewayEventHeartbeatAck
	eventError        = realtime.GatewayEventError
)

type sessionStream interface {
	Send(*sessionv1.ConnectRequest) error
	Recv() (*sessionv1.ConnectResponse, error)
	CloseSend() error
}

type Server struct {
	svcCtx          *svc.ServiceContext
	gatewayID       string
	generation      string
	checkpoints     *checkpointManager
	checkpointClose io.Closer
}

type client struct {
	server               *Server
	ws                   *websocket.Conn
	connectionID         string
	stream               sessionStream
	streamConn           io.Closer
	sourceScope          clientip.Scope
	socketLease          socketlimit.LeaseHandle
	eventWindow          time.Time
	eventCount           int
	writeMu              sync.Mutex
	heartbeatMu          sync.Mutex
	lastHeartbeat        time.Time
	highestSequence      uint64
	acknowledgedSequence uint64
	sessionID            string
	bindingEpoch         uint64
	sessionAddress       string
}

func New(svcCtx *svc.ServiceContext) *Server {
	return newServer(svcCtx, newGRPCCheckpointSender())
}

func newServer(svcCtx *svc.ServiceContext, sender checkpointSender) *Server {
	gatewayID, generation := randomID("gw"), randomID("gen")
	server := &Server{svcCtx: svcCtx, gatewayID: gatewayID, generation: generation}
	server.checkpoints = newCheckpointManager(
		sender, gatewayID, generation,
		svcCtx.Cfg.Gateway.CheckpointInterval(), svcCtx.Cfg.Gateway.CheckpointLimit(),
	)
	if closer, ok := sender.(io.Closer); ok {
		server.checkpointClose = closer
	}
	return server
}

func (s *Server) StartBackground(ctx context.Context) {
	go s.checkpoints.run(ctx)
}

func (s *Server) Close() error {
	if s.checkpointClose != nil {
		return s.checkpointClose.Close()
	}
	return nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc(s.svcCtx.Cfg.Gateway.WebSocketRoute(), s.handleWebSocket)
	return mux
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	clientAddr, err := s.svcCtx.ClientIPResolver.Resolve(r.RemoteAddr, r.Header)
	if err != nil {
		http.Error(w, "invalid client address", http.StatusBadRequest)
		return
	}
	sourceScope := clientip.SourceScope(clientAddr)
	if !s.takeHTTPRateLimit(w, r,
		gatewayratelimit.PolicyForFamily(gatewayratelimit.PolicyUpgradeIP, sourceScope.Family),
		sourceScope.Key(),
	) {
		return
	}
	var lease socketlimit.LeaseHandle
	if s.svcCtx.SocketLimiter != nil {
		scopeLimit := s.svcCtx.Cfg.Gateway.IPv6PendingHandshakeLimit()
		if sourceScope.Family == clientip.FamilyIPv4 {
			scopeLimit = s.svcCtx.Cfg.Gateway.IPv4PendingHandshakeLimit()
		}
		var allowed bool
		lease, allowed = s.svcCtx.SocketLimiter.Acquire(
			sourceScope.Key(),
			s.svcCtx.Cfg.Gateway.ConnectionLimit(),
			s.svcCtx.Cfg.Gateway.PendingHandshakeLimit(),
			scopeLimit,
		)
		if !allowed {
			http.Error(w, "websocket capacity exceeded", http.StatusTooManyRequests)
			return
		}
		defer lease.Release()
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	ws.SetReadLimit(s.svcCtx.Cfg.Gateway.MessageLimit())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &client{
		server:       s,
		ws:           ws,
		connectionID: randomID("conn"),
		sourceScope:  sourceScope,
		socketLease:  lease,
	}
	c.run(ctx)
}

func (s *Server) takeHTTPRateLimit(w http.ResponseWriter, r *http.Request, policy, key string) bool {
	if s.svcCtx.RateLimiter == nil {
		return true
	}
	decision, err := s.svcCtx.RateLimiter.Take(r.Context(), policy, key, 1)
	if err != nil {
		http.Error(w, "rate limiter unavailable", http.StatusServiceUnavailable)
		return false
	}
	if decision.Allowed {
		return true
	}
	if decision.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt(max(int64(decision.RetryAfter/time.Second), 1), 10))
	}
	http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
	return false
}

func (c *client) run(ctx context.Context) {
	defer c.close()
	if err := c.write(ctx, makeEnvelope(opHello, eventHello, helloData{
		HeartbeatIntervalMs: c.server.svcCtx.Cfg.Gateway.HeartbeatInterval().Milliseconds(),
		GatewayID:           c.server.gatewayID,
	})); err != nil {
		return
	}

	readCtx, cancel := context.WithTimeout(ctx, c.server.svcCtx.Cfg.Gateway.IdentifyTimeout())
	var first envelope
	err := wsjson.Read(readCtx, c.ws, &first)
	cancel()
	if err != nil {
		return
	}
	if first.Op != opIdentify && first.Op != opResume {
		_ = c.write(ctx, makeEnvelope(opError, eventError, errorData{
			Code: "handshake_required", Message: "first websocket message must be IDENTIFY or RESUME",
		}))
		return
	}
	if err := c.checkHandshakeRateLimit(ctx, first); err != nil {
		c.writeConnectError(ctx, first.Op, err)
		return
	}

	stream, conn, address, err := c.connect(ctx, first)
	if err != nil {
		c.writeConnectError(ctx, first.Op, err)
		return
	}
	c.stream = stream
	c.streamConn = conn
	c.sessionAddress = address
	initial, err := c.stream.Recv()
	if err != nil {
		c.writeConnectError(ctx, first.Op, err)
		return
	}
	if c.socketLease != nil {
		c.socketLease.MarkReady()
	}
	if err := c.writeSessionFrame(ctx, initial); err != nil {
		return
	}
	if c.sessionID == "" || c.bindingEpoch == 0 {
		c.writeConnectError(ctx, first.Op, errors.New("session binding metadata is missing"))
		return
	}
	c.heartbeatMu.Lock()
	c.lastHeartbeat = time.Now()
	c.heartbeatMu.Unlock()

	recvErr := make(chan error, 1)
	go func() {
		err := c.receiveSessionFrames(ctx)
		recvErr <- err
		// Unblock the websocket reader when the Session stream terminates.
		_ = c.ws.Close(websocket.StatusInternalError, "session stream closed")
	}()

	for {
		var msg envelope
		deadline := c.heartbeatDeadline()
		readCtx, cancel := context.WithDeadline(ctx, deadline)
		err := wsjson.Read(readCtx, c.ws, &msg)
		cancel()
		if err != nil {
			_ = c.sendDetach(true)
			return
		}
		if !c.allowClientEvent(time.Now()) {
			_ = c.write(ctx, makeEnvelope(opError, eventError, errorData{
				Code: "rate_limited", Message: "gateway event rate limit exceeded",
			}))
			_ = c.sendDetach(true)
			return
		}
		if msg.Op == opHeartbeat {
			if err := c.handleHeartbeat(ctx, msg); err != nil {
				_ = c.write(ctx, makeEnvelope(opError, eventError, errorData{
					Code: "operation_failed", Message: err.Error(),
				}))
			}
			continue
		}
		frame, err := c.toGatewayFrame(msg)
		if err != nil {
			_ = c.write(ctx, makeEnvelope(opError, eventError, errorData{
				Code: "operation_failed", Message: err.Error(),
			}))
			continue
		}
		if err := c.stream.Send(frame); err != nil {
			return
		}
		select {
		case err := <-recvErr:
			if err != nil {
				logx.WithContext(ctx).Errorw("receive session stream", logx.Field("error", err))
			}
			return
		default:
		}
	}
}

func (c *client) allowClientEvent(now time.Time) bool {
	if c.eventWindow.IsZero() || now.Sub(c.eventWindow) >= time.Minute {
		c.eventWindow = now
		c.eventCount = 0
	}
	c.eventCount++
	return c.eventCount <= c.server.svcCtx.Cfg.Gateway.ClientEventLimit()
}

func (c *client) checkHandshakeRateLimit(ctx context.Context, first envelope) error {
	limiter := c.server.svcCtx.RateLimiter
	if limiter == nil {
		return nil
	}
	policy := gatewayratelimit.PolicyIdentifyIP
	key := c.sourceScope.Key()
	if first.Op == opResume {
		var data resumeData
		if err := json.Unmarshal(first.D, &data); err != nil {
			return err
		}
		if strings.TrimSpace(data.SessionID) == "" {
			return errors.New("session id is required")
		}
		policy = gatewayratelimit.PolicyResumeIP
	}
	policy = gatewayratelimit.PolicyForFamily(policy, c.sourceScope.Family)
	decision, err := limiter.Take(ctx, policy, key, 1)
	if err != nil {
		return status.Error(codes.Unavailable, "rate limiter unavailable")
	}
	if !decision.Allowed {
		return status.Error(codes.ResourceExhausted, "rate limit exceeded")
	}
	if first.Op == opResume {
		var data resumeData
		_ = json.Unmarshal(first.D, &data)
		decision, err = limiter.Take(ctx, gatewayratelimit.PolicyResumeSession, data.SessionID, 1)
		if err != nil {
			return status.Error(codes.Unavailable, "rate limiter unavailable")
		}
		if !decision.Allowed {
			return status.Error(codes.ResourceExhausted, "rate limit exceeded")
		}
	}
	return nil
}

func (c *client) connect(ctx context.Context, first envelope) (sessionStream, io.Closer, string, error) {
	var (
		address string
	)
	if first.Op == opResume {
		var data resumeData
		if err := json.Unmarshal(first.D, &data); err != nil {
			return nil, nil, "", err
		}
		if strings.TrimSpace(data.SessionID) == "" {
			return nil, nil, "", errors.New("session id is required")
		}
		var err error
		address, err = c.server.svcCtx.Resolver.ResolveSession(ctx, data.SessionID)
		if err != nil {
			return nil, nil, "", err
		}
	} else {
		var err error
		address, err = c.server.svcCtx.Resolver.ResolveNode(ctx)
		if err != nil {
			return nil, nil, "", err
		}
	}
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, "", err
	}
	client := sessionv1.NewSessionServiceClient(conn)

	stream, err := client.Connect(ctx)
	if err != nil {
		_ = conn.Close()
		return nil, nil, "", err
	}
	frame, err := c.toGatewayFrame(first)
	if err != nil {
		_ = stream.CloseSend()
		_ = conn.Close()
		return nil, nil, "", err
	}
	if err := stream.Send(frame); err != nil {
		_ = conn.Close()
		return nil, nil, "", err
	}
	return stream, conn, address, nil
}

func (c *client) heartbeatDeadline() time.Time {
	c.heartbeatMu.Lock()
	defer c.heartbeatMu.Unlock()
	return c.lastHeartbeat.Add(c.server.svcCtx.Cfg.Gateway.HeartbeatTimeout())
}

func (c *client) handleHeartbeat(ctx context.Context, msg envelope) error {
	var sequence uint64
	if len(msg.D) > 0 && string(msg.D) != "null" {
		if err := json.Unmarshal(msg.D, &sequence); err != nil {
			return errors.New("heartbeat sequence is invalid")
		}
	}

	c.heartbeatMu.Lock()
	if sequence > c.highestSequence {
		c.heartbeatMu.Unlock()
		return errors.New("heartbeat sequence is ahead of gateway")
	}
	now := time.Now()
	if !c.lastHeartbeat.IsZero() && now.Before(c.lastHeartbeat.Add(c.server.svcCtx.Cfg.Gateway.HeartbeatMinimumInterval())) {
		c.heartbeatMu.Unlock()
		return errors.New("heartbeat sent before negotiated interval")
	}
	c.lastHeartbeat = now
	changed := false
	if sequence > c.acknowledgedSequence {
		c.acknowledgedSequence = sequence
		changed = true
	}
	checkpoint := connectionCheckpoint{
		address: c.sessionAddress, sessionID: c.sessionID, connectionID: c.connectionID,
		bindingEpoch: c.bindingEpoch, sequence: c.acknowledgedSequence,
	}
	c.heartbeatMu.Unlock()

	if changed {
		c.server.checkpoints.record(checkpoint)
	}
	return c.write(ctx, makeEnvelope(opHeartbeatAck, eventHeartbeatAck, nil))
}

func (c *client) receiveSessionFrames(ctx context.Context) error {
	for {
		frame, err := c.stream.Recv()
		if err != nil {
			return err
		}
		if err := c.writeSessionFrame(ctx, frame); err != nil {
			return err
		}
		if frame.GetCloseCode() != 0 {
			return c.ws.Close(websocket.StatusCode(frame.GetCloseCode()), frame.GetCloseReason())
		}
	}
}

func (c *client) toGatewayFrame(msg envelope) (*sessionv1.ConnectRequest, error) {
	frame := new(sessionv1.ConnectRequest)
	frame.SetConnectionId(c.connectionID)
	frame.SetGatewayId(c.server.gatewayID)
	frame.SetGatewayGeneration(c.server.generation)
	switch msg.Op {
	case opIdentify:
		var data identifyData
		if err := json.Unmarshal(msg.D, &data); err != nil {
			return nil, err
		}
		identify := new(sessionv1.Identify)
		identify.SetToken(data.Token)
		identify.SetDeviceType(data.DeviceType)
		identify.SetStatus(data.Status)
		identify.SetClientState(data.ClientState)
		frame.SetIdentify(identify)
	case opResume:
		var data resumeData
		if err := json.Unmarshal(msg.D, &data); err != nil {
			return nil, err
		}
		resume := new(sessionv1.Resume)
		resume.SetToken(data.Token)
		resume.SetSessionId(data.SessionID)
		resume.SetSequence(data.Sequence)
		frame.SetResume(resume)
	case opHeartbeat:
		var sequence uint64
		if len(msg.D) > 0 && string(msg.D) != "null" {
			if err := json.Unmarshal(msg.D, &sequence); err != nil {
				return nil, errors.New("heartbeat sequence is invalid")
			}
		}
		heartbeat := new(sessionv1.Heartbeat)
		heartbeat.SetSequence(sequence)
		frame.SetHeartbeat(heartbeat)
	case opPresence:
		var data presenceData
		if err := json.Unmarshal(msg.D, &data); err != nil {
			return nil, err
		}
		presence := new(sessionv1.PresenceUpdate)
		presence.SetStatus(data.Status)
		presence.SetClientState(data.ClientState)
		frame.SetPresence(presence)
	default:
		return nil, errors.New("unsupported gateway op")
	}
	return frame, nil
}

func (c *client) sendDetach(resumable bool) error {
	if c.stream == nil {
		return nil
	}
	frame := new(sessionv1.ConnectRequest)
	frame.SetConnectionId(c.connectionID)
	frame.SetGatewayId(c.server.gatewayID)
	frame.SetGatewayGeneration(c.server.generation)
	detach := new(sessionv1.Detach)
	detach.SetResumable(resumable)
	frame.SetDetach(detach)
	return c.stream.Send(frame)
}

func (c *client) writeConnectError(ctx context.Context, opcode int, err error) {
	if status.Code(err) == codes.ResourceExhausted {
		_ = c.write(ctx, makeEnvelope(opError, eventError, errorData{
			Code: "rate_limited", Message: status.Convert(err).Message(),
		}))
		return
	}
	if opcode == opResume {
		_ = c.write(ctx, envelope{Op: opInvalid, D: json.RawMessage(`false`)})
		return
	}
	message := err.Error()
	if rpcStatus, ok := status.FromError(err); ok {
		message = rpcStatus.Message()
	}
	_ = c.write(ctx, makeEnvelope(opError, eventError, errorData{
		Code: "identify_failed", Message: message,
	}))
}

func (c *client) write(ctx context.Context, msg envelope) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return wsjson.Write(ctx, c.ws, msg)
}

func (c *client) writeSessionFrame(ctx context.Context, frame *sessionv1.ConnectResponse) error {
	payload := json.RawMessage(frame.GetJsonPayload())
	if len(payload) == 0 {
		payload = json.RawMessage(`null`)
	}
	if !json.Valid(payload) {
		return errors.New("session returned invalid json payload")
	}
	if err := c.write(ctx, envelope{
		Op: int(frame.GetOpcode()),
		S:  frame.GetSequence(),
		T:  frame.GetType(),
		D:  payload,
	}); err != nil {
		return err
	}
	c.heartbeatMu.Lock()
	if frame.GetSequence() > c.highestSequence {
		c.highestSequence = frame.GetSequence()
	}
	if frame.GetSessionId() != "" {
		c.sessionID = frame.GetSessionId()
	}
	if frame.GetBindingEpoch() != 0 {
		c.bindingEpoch = frame.GetBindingEpoch()
	}
	c.heartbeatMu.Unlock()
	return nil
}

func (c *client) close() {
	c.heartbeatMu.Lock()
	address, sessionID, bindingEpoch := c.sessionAddress, c.sessionID, c.bindingEpoch
	c.heartbeatMu.Unlock()
	if c.server.checkpoints != nil && address != "" && sessionID != "" {
		c.server.checkpoints.remove(address, sessionID, c.connectionID, bindingEpoch)
	}
	if c.stream != nil {
		_ = c.stream.CloseSend()
	}
	if c.streamConn != nil {
		_ = c.streamConn.Close()
	}
	_ = c.ws.Close(websocket.StatusNormalClosure, "")
}

func randomID(prefix string) string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return prefix + "-" + time.Now().Format("20060102150405.000000000")
	}
	return prefix + "-" + hex.EncodeToString(value[:])
}
