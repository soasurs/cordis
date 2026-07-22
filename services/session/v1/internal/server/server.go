package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	mathrand "math/rand/v2"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/semaphore"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/observability"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/pkg/sessionregistry"
	"github.com/soasurs/cordis/services/session/v1/internal/store"
	"github.com/soasurs/cordis/services/session/v1/internal/svc"
	sessionratelimit "github.com/soasurs/cordis/services/session/v1/ratelimit"
)

const (
	opDispatch     = 0
	opHeartbeatAck = 11
	opInvalid      = 9
	leaseJitter    = 0.2
	leaseBatchSize = 500
)

type replayEntry struct {
	sequence uint64
	frame    *sessionv1.ConnectResponse
}

type pendingDispatch struct {
	eventType string
	payload   []byte
}

type leaseRefreshOutcome struct {
	ownerFailures       int
	presenceFailures    int
	ownerErrorType      string
	presenceErrorType   string
	completedBatchCount int
}

type binding struct {
	id    string
	epoch uint64
	send  chan *sessionv1.ConnectResponse
	done  chan struct{}
	once  sync.Once
}

func (b *binding) close() {
	b.once.Do(func() { close(b.done) })
}

type logicalSession struct {
	mu sync.Mutex

	id                      string
	userID                  int64
	authSessionID           int64
	gatewayID               string
	gatewayGeneration       string
	deviceType              string
	status                  presencev1.PresenceStatus
	clientState             presencev1.ClientState
	sequence                uint64
	ackedSequence           uint64
	replay                  []replayEntry
	replayFloor             uint64
	guilds                  map[int64]struct{}
	binding                 *binding
	bindingEpoch            uint64
	detachedAt              time.Time
	presenceWindow          time.Time
	presenceUpdates         int
	initializing            bool
	pendingDispatches       []pendingDispatch
	pendingDispatchBytes    int64
	pendingDispatchOverflow bool
}

type Server struct {
	sessionv1.UnimplementedSessionServiceServer

	svcCtx     *svc.ServiceContext
	nodeID     string
	generation string
	rpcAddress string

	mu       sync.RWMutex
	sessions map[string]*logicalSession
	users    map[int64]map[*logicalSession]struct{}
	guilds   map[int64]map[*logicalSession]struct{}
	draining atomic.Bool

	visibilityMu        sync.RWMutex
	visibilityUsers     map[int64]*userVisibilityState
	visibilityReloads   singleflight.Group
	visibilityReloadSem *semaphore.Weighted

	routeMu         sync.Mutex
	publishedRoutes map[store.Route]struct{}
	tracer          trace.Tracer

	dedup *dedupStore
}

func New(svcCtx *svc.ServiceContext) *Server {
	nodeID := strings.TrimSpace(svcCtx.Cfg.Node.ID)
	if nodeID == "" {
		nodeID, _ = os.Hostname()
	}
	if nodeID == "" {
		nodeID = randomID("session-node")
	}
	rpcAddress := strings.TrimSpace(svcCtx.Cfg.Node.AdvertiseAddress)
	if rpcAddress == "" {
		rpcAddress = svcCtx.Cfg.ListenOn
	}
	return &Server{
		svcCtx:              svcCtx,
		nodeID:              nodeID,
		generation:          randomID("gen"),
		rpcAddress:          rpcAddress,
		sessions:            make(map[string]*logicalSession),
		users:               make(map[int64]map[*logicalSession]struct{}),
		guilds:              make(map[int64]map[*logicalSession]struct{}),
		visibilityUsers:     make(map[int64]*userVisibilityState),
		visibilityReloadSem: semaphore.NewWeighted(svcCtx.Cfg.Node.SnapshotReloadLimit()),
		publishedRoutes:     make(map[store.Route]struct{}),
		dedup:               newDedupStore(),
	}
}

func (s *Server) StartBackground(ctx context.Context) {
	go s.refreshNode(ctx)
	go s.refreshRoutes(ctx)
	go s.refreshSessionLeaseLoop(ctx)
	go s.cleanupDetached(ctx)
	go s.dedup.start(ctx)
}

func (s *Server) Drain(ctx context.Context) {
	if !s.draining.CompareAndSwap(false, true) {
		return
	}
	_ = s.registerNode(ctx, sessionregistry.StatusDraining)

	s.mu.RLock()
	sessions := make([]*logicalSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.mu.RUnlock()

	var interval time.Duration
	if len(sessions) > 0 {
		interval = s.svcCtx.Cfg.Node.DrainWindow() / time.Duration(len(sessions))
	}
	for _, session := range sessions {
		session.mu.Lock()
		if session.binding != nil {
			frame := new(sessionv1.ConnectResponse)
			frame.SetOpcode(opInvalid)
			frame.SetJsonPayload(`false`)
			frame.SetCloseCode(1012)
			frame.SetCloseReason("session node draining")
			_ = enqueue(session.binding, frame)
		}
		session.mu.Unlock()
		if interval > 0 {
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}

func (s *Server) Connect(stream sessionv1.SessionService_ConnectServer) (returnErr error) {
	closeReason := "handshake_failed"
	sessionGatewayStreamsActive.Inc()
	defer func() {
		sessionGatewayStreamsActive.Dec()
		sessionGatewayStreamClosesTotal.WithLabelValues(closeReason).Inc()
	}()
	first, err := stream.Recv()
	if err != nil {
		closeReason = sessionStreamCloseReason(err)
		return err
	}
	observeSessionGatewayFrame("gateway_in", proto.Size(first))
	operation, expectedEvent := "", ""
	switch {
	case first.GetIdentify() != nil:
		operation, expectedEvent = "identify", realtime.GatewayEventReady
	case first.GetResume() != nil:
		operation, expectedEvent = "resume", realtime.GatewayEventResumed
	}
	handshakeCtx := stream.Context()
	var handshakeSpan trace.Span
	handshakeOpen := false
	handshakeStarted := time.Time{}
	if operation != "" {
		tracer := s.tracer
		if tracer == nil {
			tracer = otel.Tracer(observability.SessionInstrumentationName)
		}
		handshakeCtx, handshakeSpan = tracer.Start(
			stream.Context(),
			"session."+operation,
			trace.WithAttributes(attribute.String("cordis.session.operation", operation)),
		)
		handshakeStarted = time.Now()
		handshakeOpen = true
	}
	finishHandshake := func(err error) {
		if !handshakeOpen {
			return
		}
		result := sessionHandshakeResult(err)
		observeSessionHandshake(handshakeStarted, operation, err)
		handshakeSpan.SetAttributes(attribute.String("cordis.session.result", result))
		if err != nil {
			handshakeSpan.SetStatus(otelcodes.Error, result)
		}
		handshakeSpan.End()
		handshakeOpen = false
	}
	defer func() { finishHandshake(returnErr) }()

	if strings.TrimSpace(first.GetConnectionId()) == "" {
		return status.Error(codes.InvalidArgument, "connection id is required")
	}
	if strings.TrimSpace(first.GetGatewayId()) == "" || strings.TrimSpace(first.GetGatewayGeneration()) == "" {
		return status.Error(codes.InvalidArgument, "gateway identity is required")
	}
	if s.draining.Load() {
		return status.Error(codes.Unavailable, "session node is draining")
	}

	var session *logicalSession
	switch {
	case first.GetIdentify() != nil:
		session, err = s.identify(
			handshakeCtx,
			first.GetConnectionId(),
			first.GetGatewayId(),
			first.GetGatewayGeneration(),
			first.GetIdentify(),
		)
	case first.GetResume() != nil:
		session, err = s.resume(
			handshakeCtx,
			first.GetConnectionId(),
			first.GetGatewayId(),
			first.GetGatewayGeneration(),
			first.GetResume(),
		)
	default:
		err = status.Error(codes.InvalidArgument, "first frame must identify or resume")
	}
	if err != nil {
		return err
	}

	session.mu.Lock()
	current := session.binding
	session.mu.Unlock()
	if current == nil {
		return status.Error(codes.FailedPrecondition, "session binding is missing")
	}
	closeReason = "binding_closed"

	runtimeCtx := trace.ContextWithSpanContext(stream.Context(), trace.SpanContext{})
	recv := make(chan error, 1)
	go func() {
		recv <- s.receiveFrames(runtimeCtx, stream, session, current)
	}()

	for {
		select {
		case frame := <-current.send:
			frame.SetSessionId(session.id)
			frame.SetBindingEpoch(current.epoch)
			if err := sendSessionGatewayFrame(stream, frame); err != nil {
				closeReason = "send_failure"
				s.detach(session, current, true)
				return err
			}
			if frame.GetType() == expectedEvent {
				finishHandshake(nil)
			}
		case err := <-recv:
			closeReason = sessionStreamCloseReason(err)
			s.detach(session, current, true)
			return err
		case <-current.done:
			return nil
		case <-stream.Context().Done():
			closeReason = "canceled"
			s.detach(session, current, true)
			return stream.Context().Err()
		}
	}
}

func (s *Server) SyncGatewayConnections(
	_ context.Context,
	req *sessionv1.SyncGatewayConnectionsRequest,
) (*sessionv1.SyncGatewayConnectionsResponse, error) {
	if strings.TrimSpace(req.GetGatewayId()) == "" || strings.TrimSpace(req.GetGatewayGeneration()) == "" {
		return nil, status.Error(codes.InvalidArgument, "gateway id and generation are required")
	}

	var applied int32
	for _, checkpoint := range req.GetCheckpoints() {
		s.mu.RLock()
		session := s.sessions[checkpoint.GetSessionId()]
		s.mu.RUnlock()
		if session == nil {
			continue
		}

		session.mu.Lock()
		binding := session.binding
		if binding == nil ||
			binding.id != checkpoint.GetConnectionId() ||
			binding.epoch != checkpoint.GetBindingEpoch() ||
			session.gatewayID != req.GetGatewayId() ||
			session.gatewayGeneration != req.GetGatewayGeneration() ||
			checkpoint.GetAcknowledgedSequence() > session.sequence {
			session.mu.Unlock()
			continue
		}
		acknowledgeLocked(session, checkpoint.GetAcknowledgedSequence())
		session.mu.Unlock()
		applied++
	}

	resp := new(sessionv1.SyncGatewayConnectionsResponse)
	resp.SetApplied(applied)
	return resp, nil
}

func (s *Server) identify(
	ctx context.Context,
	connectionID, gatewayID, gatewayGeneration string,
	data *sessionv1.Identify,
) (*logicalSession, error) {
	if strings.TrimSpace(data.GetToken()) == "" {
		return nil, status.Error(codes.Unauthenticated, "token is required")
	}
	authReq := new(authenticatorv1.VerifyAccessTokenRequest)
	authReq.SetAccessToken(data.GetToken())
	auth, err := s.svcCtx.AuthenticatorClient.VerifyAccessToken(ctx, authReq)
	if err != nil {
		return nil, err
	}
	if !auth.GetOk() || auth.GetUserId() == 0 || auth.GetSessionId() == 0 {
		return nil, status.Error(codes.Unauthenticated, "access token rejected")
	}
	if err := s.checkIdentifyRateLimits(ctx, auth.GetUserId(), auth.GetSessionId()); err != nil {
		return nil, err
	}

	session := &logicalSession{
		id:                randomID("sess"),
		userID:            auth.GetUserId(),
		authSessionID:     auth.GetSessionId(),
		gatewayID:         gatewayID,
		gatewayGeneration: gatewayGeneration,
		deviceType:        data.GetDeviceType(),
		status:            statusFromString(data.GetStatus()),
		clientState:       clientStateFromString(data.GetClientState()),
		guilds:            make(map[int64]struct{}),
		replay:            make([]replayEntry, 0, min(s.svcCtx.Cfg.Node.ReplayLimit(), 64)),
		initializing:      true,
	}
	readyGuilds, visibilitySnapshots, err := s.loadReadyGuilds(ctx, session.userID)
	if err != nil {
		return nil, err
	}
	for guildID := range visibilitySnapshots {
		session.guilds[guildID] = struct{}{}
	}
	b := newBinding(connectionID, 1, s.svcCtx.Cfg.Node.QueueSize())
	session.binding = b
	session.bindingEpoch = 1

	s.addSession(session, visibilitySnapshots)
	if err := s.refreshOwner(ctx, session); err != nil {
		s.removeSession(ctx, session)
		return nil, err
	}
	if err := s.registerPresence(ctx, session); err != nil {
		s.removeSession(ctx, session)
		return nil, err
	}
	s.refreshAllRoutes(ctx)

	messageReq := new(messagev1.GetUserReadyStateRequest)
	messageReq.SetUserId(session.userID)
	messageReq.SetGuildChannelIds(readyGuildTextChannelIDs(readyGuilds))
	messageReady, err := s.svcCtx.MessageClient.GetUserReadyState(ctx, messageReq)
	if err != nil {
		s.removeSession(ctx, session)
		return nil, err
	}
	ready, err := marshalReady(session, auth.GetExpiresAt(), readyGuilds, messageReady, s.nodeID)
	if err != nil {
		s.removeSession(ctx, session)
		return nil, status.Error(codes.Internal, "marshal ready payload")
	}
	session.mu.Lock()
	if session.pendingDispatchOverflow {
		session.mu.Unlock()
		s.removeSession(ctx, session)
		return nil, status.Error(codes.ResourceExhausted, "ready event buffer overflow")
	}
	s.appendDispatchLocked(session, realtime.GatewayEventReady, ready)
	for _, pending := range session.pendingDispatches {
		s.appendDispatchLocked(session, pending.eventType, pending.payload)
	}
	session.pendingDispatches = nil
	session.pendingDispatchBytes = 0
	session.initializing = false
	session.mu.Unlock()
	return session, nil
}

func (s *Server) checkIdentifyRateLimits(ctx context.Context, userID, authSessionID int64) error {
	if s.svcCtx.RateLimiter == nil {
		return nil
	}
	checks := []struct {
		policy string
		key    string
	}{
		{policy: sessionratelimit.PolicyIdentifyUser, key: strconv.FormatInt(userID, 10)},
		{policy: sessionratelimit.PolicyIdentifyAuthSession, key: strconv.FormatInt(authSessionID, 10)},
	}
	for _, check := range checks {
		decision, err := s.svcCtx.RateLimiter.Take(ctx, check.policy, check.key, 1)
		if err != nil {
			return status.Error(codes.Internal, "rate limiter unavailable")
		}
		if !decision.Allowed {
			return status.Error(codes.ResourceExhausted, "identify rate limit exceeded")
		}
	}
	return nil
}

func (s *Server) resume(
	ctx context.Context,
	connectionID, gatewayID, gatewayGeneration string,
	data *sessionv1.Resume,
) (*logicalSession, error) {
	if strings.TrimSpace(data.GetToken()) == "" || strings.TrimSpace(data.GetSessionId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "token and session id are required")
	}
	s.mu.RLock()
	session := s.sessions[data.GetSessionId()]
	s.mu.RUnlock()
	if session == nil {
		return nil, status.Error(codes.NotFound, "session not found")
	}

	authReq := new(authenticatorv1.VerifyAccessTokenRequest)
	authReq.SetAccessToken(data.GetToken())
	auth, err := s.svcCtx.AuthenticatorClient.VerifyAccessToken(ctx, authReq)
	if err != nil {
		return nil, err
	}
	if !auth.GetOk() || auth.GetUserId() != session.userID || auth.GetSessionId() != session.authSessionID {
		return nil, status.Error(codes.Unauthenticated, "resume token rejected")
	}

	session.mu.Lock()
	if data.GetSequence() > session.sequence {
		session.mu.Unlock()
		return nil, status.Error(codes.FailedPrecondition, "session is not resumable")
	}
	if data.GetSequence() < session.replayFloor || data.GetSequence() < session.ackedSequence {
		session.mu.Unlock()
		return nil, status.Error(codes.FailedPrecondition, "replay sequence expired")
	}

	old := session.binding
	replayCount := 0
	for _, entry := range session.replay {
		if entry.sequence > data.GetSequence() {
			replayCount++
		}
	}
	session.bindingEpoch++
	b := newBinding(connectionID, session.bindingEpoch, max(s.svcCtx.Cfg.Node.QueueSize(), replayCount+1))
	session.binding = b
	session.gatewayID = gatewayID
	session.gatewayGeneration = gatewayGeneration
	session.detachedAt = time.Time{}
	if old != nil {
		old.close()
	}
	for _, entry := range session.replay {
		if entry.sequence > data.GetSequence() {
			b.send <- cloneFrame(entry.frame)
		}
	}
	s.appendDispatchLocked(session, realtime.GatewayEventResumed, []byte(`{}`))
	session.mu.Unlock()
	if err := s.refreshOwner(ctx, session); err != nil {
		return nil, err
	}
	if err := s.refreshPresence(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *Server) receiveFrames(
	ctx context.Context,
	stream sessionv1.SessionService_ConnectServer,
	session *logicalSession,
	binding *binding,
) error {
	for {
		frame, err := stream.Recv()
		if err != nil {
			return err
		}
		observeSessionGatewayFrame("gateway_in", proto.Size(frame))
		if frame.GetConnectionId() != binding.id {
			return status.Error(codes.PermissionDenied, "connection id mismatch")
		}
		switch {
		case frame.GetHeartbeat() != nil:
			if err := s.heartbeat(ctx, session, binding, frame.GetHeartbeat().GetSequence()); err != nil {
				return err
			}
		case frame.GetPresence() != nil:
			if err := s.updatePresence(ctx, session, binding, frame.GetPresence()); err != nil {
				return err
			}
		case frame.GetDetach() != nil:
			s.detach(session, binding, frame.GetDetach().GetResumable())
			return nil
		default:
			return status.Error(codes.InvalidArgument, "unsupported session frame")
		}
	}
}

func sessionHandshakeResult(err error) string {
	if err == nil {
		return "success"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	switch status.Code(err) {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.NotFound, codes.Unauthenticated, codes.PermissionDenied:
		return "rejected"
	case codes.DeadlineExceeded:
		return "timeout"
	case codes.Canceled:
		return "canceled"
	case codes.ResourceExhausted:
		return "rate_limited"
	case codes.Unavailable:
		return "unavailable"
	default:
		return "internal"
	}
}

func (s *Server) heartbeat(_ context.Context, session *logicalSession, binding *binding, sequence uint64) error {
	session.mu.Lock()
	if session.binding != binding {
		session.mu.Unlock()
		return status.Error(codes.Aborted, "stale session binding")
	}
	if sequence > session.sequence {
		session.mu.Unlock()
		return status.Error(codes.InvalidArgument, "heartbeat sequence is ahead of session")
	}
	acknowledgeLocked(session, sequence)
	session.mu.Unlock()
	ack := new(sessionv1.ConnectResponse)
	ack.SetOpcode(opHeartbeatAck)
	ack.SetJsonPayload(`null`)
	return enqueue(binding, ack)
}

func acknowledgeLocked(session *logicalSession, sequence uint64) {
	if sequence <= session.ackedSequence {
		return
	}
	session.ackedSequence = sequence
	cut := 0
	for cut < len(session.replay) && session.replay[cut].sequence <= sequence {
		cut++
	}
	session.replay = append(session.replay[:0], session.replay[cut:]...)
}

func (s *Server) updatePresence(ctx context.Context, session *logicalSession, binding *binding, data *sessionv1.PresenceUpdate) error {
	statusValue := statusFromString(data.GetStatus())
	clientState := clientStateFromString(data.GetClientState())
	now := time.Now()

	session.mu.Lock()
	if session.binding != binding {
		session.mu.Unlock()
		return status.Error(codes.Aborted, "stale session binding")
	}
	if session.status == statusValue && session.clientState == clientState {
		session.mu.Unlock()
		return nil
	}
	if session.presenceWindow.IsZero() || now.Sub(session.presenceWindow) >= s.svcCtx.Cfg.Node.PresenceUpdateWindow() {
		session.presenceWindow = now
		session.presenceUpdates = 0
	}
	session.presenceUpdates++
	if session.presenceUpdates > s.svcCtx.Cfg.Node.PresenceUpdateLimit() {
		session.mu.Unlock()
		return status.Error(codes.ResourceExhausted, "presence update rate limit exceeded")
	}
	userID := session.userID
	session.mu.Unlock()

	if err := s.takeOperationRateLimit(
		ctx, sessionratelimit.PolicyPresenceUser, strconv.FormatInt(userID, 10), 1,
		"presence update rate limit exceeded",
	); err != nil {
		return err
	}

	session.mu.Lock()
	if session.binding != binding {
		session.mu.Unlock()
		return status.Error(codes.Aborted, "stale session binding")
	}
	if session.status == statusValue && session.clientState == clientState {
		session.mu.Unlock()
		return nil
	}
	oldStatus, oldClientState := session.status, session.clientState
	session.status = statusValue
	session.clientState = clientState
	session.mu.Unlock()

	if err := s.updatePresenceRPC(ctx, session); err != nil {
		session.mu.Lock()
		if session.status == statusValue && session.clientState == clientState {
			session.status = oldStatus
			session.clientState = oldClientState
		}
		session.mu.Unlock()
		return err
	}
	return nil
}

func (s *Server) takeOperationRateLimit(
	ctx context.Context,
	policy, key string,
	cost int64,
	message string,
) error {
	if cost == 0 || s.svcCtx.RateLimiter == nil {
		return nil
	}
	decision, err := s.svcCtx.RateLimiter.Take(ctx, policy, key, cost)
	if err != nil {
		return status.Error(codes.Internal, "rate limiter unavailable")
	}
	if !decision.Allowed {
		return status.Error(codes.ResourceExhausted, message)
	}
	return nil
}

func (s *Server) detach(session *logicalSession, binding *binding, resumable bool) {
	session.mu.Lock()
	if session.binding != binding {
		session.mu.Unlock()
		return
	}
	session.binding = nil
	session.detachedAt = time.Now()
	session.mu.Unlock()
	binding.close()
	if !resumable {
		s.removeSession(context.Background(), session)
		return
	}
	_ = s.refreshOwner(context.Background(), session)
}

func (s *Server) appendDispatchLocked(session *logicalSession, eventType string, payload []byte) {
	session.sequence++
	frame := new(sessionv1.ConnectResponse)
	frame.SetOpcode(opDispatch)
	frame.SetSequence(session.sequence)
	frame.SetType(eventType)
	frame.SetJsonPayload(string(payload))
	session.replay = append(session.replay, replayEntry{sequence: session.sequence, frame: frame})
	if len(session.replay) > s.svcCtx.Cfg.Node.ReplayLimit() {
		overflow := len(session.replay) - s.svcCtx.Cfg.Node.ReplayLimit()
		session.replayFloor = session.replay[overflow-1].sequence
		session.replay = session.replay[overflow:]
	}
	if session.binding != nil {
		if err := enqueue(session.binding, cloneFrame(frame)); err != nil {
			session.binding.close()
			session.binding = nil
			session.detachedAt = time.Now()
		}
	}
}

func enqueue(binding *binding, frame *sessionv1.ConnectResponse) error {
	select {
	case binding.send <- frame:
		return nil
	case <-binding.done:
		return errors.New("session binding closed")
	default:
		sessionBindingQueueOverflowsTotal.Inc()
		return errors.New("session binding queue full")
	}
}

func sendSessionGatewayFrame(stream sessionv1.SessionService_ConnectServer, frame *sessionv1.ConnectResponse) error {
	if err := stream.Send(frame); err != nil {
		return err
	}
	observeSessionGatewayFrame("gateway_out", proto.Size(frame))
	return nil
}

func newBinding(id string, epoch uint64, queueSize int) *binding {
	return &binding{id: id, epoch: epoch, send: make(chan *sessionv1.ConnectResponse, queueSize), done: make(chan struct{})}
}

func cloneFrame(frame *sessionv1.ConnectResponse) *sessionv1.ConnectResponse {
	cloned := new(sessionv1.ConnectResponse)
	cloned.SetOpcode(frame.GetOpcode())
	cloned.SetSequence(frame.GetSequence())
	cloned.SetType(frame.GetType())
	cloned.SetJsonPayload(frame.GetJsonPayload())
	cloned.SetCloseCode(frame.GetCloseCode())
	cloned.SetCloseReason(frame.GetCloseReason())
	return cloned
}

func (s *Server) addSession(session *logicalSession, snapshots map[int64]*visibilitySnapshot) {
	s.mu.Lock()
	s.sessions[session.id] = session
	addIndex(s.users, session.userID, session)
	for guildID := range session.guilds {
		addIndex(s.guilds, guildID, session)
	}
	s.mu.Unlock()
	s.retainVisibilitySnapshots(session.userID, snapshots)
}

func addIndex(index map[int64]map[*logicalSession]struct{}, id int64, session *logicalSession) {
	set := index[id]
	if set == nil {
		set = make(map[*logicalSession]struct{})
		index[id] = set
	}
	set[session] = struct{}{}
}

func removeIndex(index map[int64]map[*logicalSession]struct{}, id int64, session *logicalSession) {
	delete(index[id], session)
	if len(index[id]) == 0 {
		delete(index, id)
	}
}

func (s *Server) removeSession(ctx context.Context, session *logicalSession) {
	session.mu.Lock()
	if session.binding != nil {
		session.binding.close()
		session.binding = nil
	}
	guildIDs := mapKeys(session.guilds)
	session.mu.Unlock()

	s.mu.Lock()
	if current := s.sessions[session.id]; current != session {
		s.mu.Unlock()
		return
	}
	delete(s.sessions, session.id)
	removeIndex(s.users, session.userID, session)
	for _, guildID := range guildIDs {
		removeIndex(s.guilds, guildID, session)
	}
	s.mu.Unlock()
	s.releaseVisibilitySnapshots(session.userID)
	_ = s.svcCtx.Store.DeleteOwner(ctx, session.id, s.nodeID, s.generation)
	s.removePresence(ctx, session, guildIDs)
	s.refreshAllRoutes(ctx)
}

func (s *Server) authorizeChannel(ctx context.Context, userID, channelID int64) (bool, error) {
	req := new(guildv1.AuthorizeGuildChannelRequest)
	req.SetUserId(userID)
	req.SetChannelId(channelID)
	req.SetPermission(uint64(guildv1.GuildPermission_GUILD_PERMISSION_VIEW_CHANNEL))
	resp, err := s.svcCtx.GuildClient.AuthorizeGuildChannel(ctx, req)
	if err != nil {
		return false, err
	}
	return resp.GetAllowed(), nil
}

func (s *Server) refreshOwner(ctx context.Context, session *logicalSession) error {
	return s.svcCtx.Store.SetOwner(ctx, store.Owner{
		SessionID: session.id, NodeID: s.nodeID, Generation: s.generation,
	}, s.svcCtx.Cfg.Node.ResumeTTL())
}

// presenceGuildIDs snapshots the session's guild memberships for presence
// transition fan-out.
func presenceGuildIDs(session *logicalSession) []int64 {
	session.mu.Lock()
	defer session.mu.Unlock()
	return mapKeys(session.guilds)
}

func (s *Server) registerPresence(ctx context.Context, session *logicalSession) error {
	req := new(presencev1.RegisterUserSessionRequest)
	req.SetUserId(session.userID)
	req.SetSessionId(session.id)
	req.SetGatewayId(session.gatewayID)
	req.SetGeneration(session.gatewayGeneration)
	req.SetDeviceType(session.deviceType)
	req.SetStatus(session.status)
	req.SetClientState(session.clientState)
	req.SetGuildIds(presenceGuildIDs(session))
	_, err := s.svcCtx.PresenceClient.RegisterUserSession(ctx, req)
	return err
}

func (s *Server) refreshPresence(ctx context.Context, session *logicalSession) error {
	session.mu.Lock()
	statusValue, clientState := session.status, session.clientState
	session.mu.Unlock()
	req := new(presencev1.RefreshUserSessionRequest)
	req.SetUserId(session.userID)
	req.SetSessionId(session.id)
	req.SetGatewayId(session.gatewayID)
	req.SetGeneration(session.gatewayGeneration)
	req.SetDeviceType(session.deviceType)
	req.SetStatus(statusValue)
	req.SetClientState(clientState)
	req.SetGuildIds(presenceGuildIDs(session))
	_, err := s.svcCtx.PresenceClient.RefreshUserSession(ctx, req)
	return err
}

func (s *Server) updatePresenceRPC(ctx context.Context, session *logicalSession) error {
	session.mu.Lock()
	statusValue, clientState := session.status, session.clientState
	session.mu.Unlock()
	req := new(presencev1.UpdateUserPresenceRequest)
	req.SetUserId(session.userID)
	req.SetSessionId(session.id)
	req.SetStatus(statusValue)
	req.SetClientState(clientState)
	req.SetGuildIds(presenceGuildIDs(session))
	_, err := s.svcCtx.PresenceClient.UpdateUserPresence(ctx, req)
	return err
}

func (s *Server) removePresence(ctx context.Context, session *logicalSession, guildIDs []int64) {
	req := new(presencev1.RemoveUserSessionRequest)
	req.SetUserId(session.userID)
	req.SetSessionId(session.id)
	req.SetGuildIds(guildIDs)
	_, _ = s.svcCtx.PresenceClient.RemoveUserSession(ctx, req)
}

func (s *Server) refreshNode(ctx context.Context) {
	ticker := time.NewTicker(s.svcCtx.Cfg.Node.HeartbeatInterval())
	defer ticker.Stop()
	for {
		nodeStatus := sessionregistry.StatusReady
		if s.draining.Load() {
			nodeStatus = sessionregistry.StatusDraining
		}
		err := s.registerNode(ctx, nodeStatus)
		if err != nil && ctx.Err() == nil {
			logx.WithContext(ctx).Errorw("register session node", logx.Field("error", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) registerNode(ctx context.Context, nodeStatus string) error {
	return s.svcCtx.SessionRegistry.Register(ctx, sessionregistry.Node{
		ID: s.nodeID, Generation: s.generation, RPCAddress: s.rpcAddress, Status: nodeStatus,
	}, s.svcCtx.Cfg.Node.NodeTTL())
}

func (s *Server) refreshRoutes(ctx context.Context) {
	ticker := time.NewTicker(s.svcCtx.Cfg.Node.RouteRefreshInterval())
	defer ticker.Stop()
	for {
		s.refreshAllRoutes(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) refreshSessionLeaseLoop(ctx context.Context) {
	nextCycle := time.Now().Add(jitterDuration(s.svcCtx.Cfg.Node.SessionLeaseRefreshInterval()))
	for {
		if !waitUntil(ctx, nextCycle) {
			return
		}
		cycleStart := time.Now()
		nextCycle = cycleStart.Add(jitterDuration(s.svcCtx.Cfg.Node.SessionLeaseRefreshInterval()))
		s.refreshSessionLeases(ctx)
	}
}

func jitterDuration(base time.Duration) time.Duration {
	if base <= 0 {
		return time.Millisecond
	}
	factor := 1 - leaseJitter + mathrand.Float64()*(2*leaseJitter)
	return time.Duration(float64(base) * factor)
}

func waitUntil(ctx context.Context, target time.Time) bool {
	delay := time.Until(target)
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func batchRefreshOffset(batch, batchCount int, spread time.Duration) time.Duration {
	if batchCount <= 1 || spread <= 0 {
		return 0
	}
	slot := spread / time.Duration(batchCount)
	if slot <= 0 {
		return 0
	}
	return time.Duration(batch)*slot + time.Duration(mathrand.Int64N(int64(slot)))
}

func (s *Server) refreshSessionLeases(ctx context.Context) {
	s.refreshSessionLeasesWithSpread(ctx, s.svcCtx.Cfg.Node.SessionLeaseSpreadWindow())
}

func (s *Server) refreshSessionLeasesWithSpread(ctx context.Context, spread time.Duration) {
	tracer := s.tracer
	if tracer == nil {
		tracer = otel.Tracer(observability.SessionInstrumentationName)
	}
	ctx, span := tracer.Start(ctx, "session.lease.refresh", trace.WithSpanKind(trace.SpanKindInternal))
	outcome := leaseRefreshOutcome{}
	defer func() {
		result := "success"
		if ctx.Err() != nil {
			result = "canceled"
		} else if outcome.ownerFailures > 0 || outcome.presenceFailures > 0 {
			result = "partial_failure"
		}
		span.SetAttributes(
			attribute.Int("cordis.session.lease.completed_batch_count", outcome.completedBatchCount),
			attribute.Int("cordis.session.lease.owner_failure_count", outcome.ownerFailures),
			attribute.Int("cordis.session.lease.presence_failure_count", outcome.presenceFailures),
			attribute.String("cordis.session.lease.owner_error_type", defaultLeaseErrorType(outcome.ownerErrorType)),
			attribute.String("cordis.session.lease.presence_error_type", defaultLeaseErrorType(outcome.presenceErrorType)),
			attribute.String("cordis.session.lease.result", result),
		)
		if result != "success" {
			span.SetStatus(otelcodes.Error, result)
		}
		span.End()
	}()

	s.mu.RLock()
	sessions := make([]*logicalSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.mu.RUnlock()
	batchCount := (len(sessions) + leaseBatchSize - 1) / leaseBatchSize
	span.SetAttributes(
		attribute.Int("cordis.session.lease.session_count", len(sessions)),
		attribute.Int("cordis.session.lease.batch_count", batchCount),
		attribute.Int("cordis.session.lease.batch_size", leaseBatchSize),
		attribute.Int64("cordis.session.lease.interval_ms", s.svcCtx.Cfg.Node.SessionLeaseRefreshInterval().Milliseconds()),
		attribute.Int64("cordis.session.lease.spread_ms", spread.Milliseconds()),
	)
	cycleStart := time.Now()
	for batch, start := 0, 0; start < len(sessions); batch, start = batch+1, start+leaseBatchSize {
		if !waitUntil(ctx, cycleStart.Add(batchRefreshOffset(batch, batchCount, spread))) {
			return
		}
		end := min(start+leaseBatchSize, len(sessions))
		outcome.merge(s.refreshSessionLeaseBatch(ctx, sessions[start:end]))
		outcome.completedBatchCount++
	}
}

func (s *Server) refreshSessionLeaseBatch(ctx context.Context, sessions []*logicalSession) leaseRefreshOutcome {
	outcome := leaseRefreshOutcome{}
	owners := make([]store.Owner, 0, len(sessions))
	for _, session := range sessions {
		owners = append(owners, store.Owner{SessionID: session.id, NodeID: s.nodeID, Generation: s.generation})
	}
	if err := s.svcCtx.Store.SetOwners(ctx, owners, s.svcCtx.Cfg.Node.ResumeTTL()); err != nil {
		outcome.recordOwnerFailure(err)
		if ctx.Err() == nil {
			logx.WithContext(ctx).Errorw("refresh session owners", logx.Field("count", len(owners)), logx.Field("error", err))
		}
	}
	req := new(presencev1.RefreshUserSessionsRequest)
	items := make([]*presencev1.RefreshUserSessionRequest, 0, len(sessions))
	byID := make(map[string]*logicalSession, len(sessions))
	for _, session := range sessions {
		items = append(items, presenceRefreshRequest(session))
		byID[session.id] = session
	}
	req.SetSessions(items)
	resp, err := s.svcCtx.PresenceClient.RefreshUserSessions(ctx, req)
	if err != nil {
		outcome.recordPresenceFailure(err)
		if ctx.Err() == nil {
			logx.WithContext(ctx).Errorw("refresh session presences", logx.Field("count", len(items)), logx.Field("error", err))
		}
		return outcome
	}
	for _, sessionID := range resp.GetMissingSessionIds() {
		if session := byID[sessionID]; session != nil {
			if err := s.registerPresence(ctx, session); err != nil {
				outcome.recordPresenceFailure(err)
				if ctx.Err() == nil {
					logx.WithContext(ctx).Errorw("register missing session presence", logx.Field("session_id", sessionID), logx.Field("error", err))
				}
			}
		}
	}
	return outcome
}

func (o *leaseRefreshOutcome) merge(other leaseRefreshOutcome) {
	o.ownerFailures += other.ownerFailures
	o.presenceFailures += other.presenceFailures
	if o.ownerErrorType == "" {
		o.ownerErrorType = other.ownerErrorType
	}
	if o.presenceErrorType == "" {
		o.presenceErrorType = other.presenceErrorType
	}
}

func (o *leaseRefreshOutcome) recordOwnerFailure(err error) {
	o.ownerFailures++
	if o.ownerErrorType == "" {
		o.ownerErrorType = leaseRefreshErrorType(err)
	}
}

func (o *leaseRefreshOutcome) recordPresenceFailure(err error) {
	o.presenceFailures++
	if o.presenceErrorType == "" {
		o.presenceErrorType = leaseRefreshErrorType(err)
	}
}

func defaultLeaseErrorType(errorType string) string {
	if errorType == "" {
		return "none"
	}
	return errorType
}

func leaseRefreshErrorType(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	switch status.Code(err) {
	case codes.Canceled:
		return "canceled"
	case codes.DeadlineExceeded:
		return "timeout"
	case codes.Unavailable:
		return "unavailable"
	case codes.ResourceExhausted:
		return "resource_exhausted"
	default:
		return "internal"
	}
}

func presenceRefreshRequest(session *logicalSession) *presencev1.RefreshUserSessionRequest {
	session.mu.Lock()
	statusValue, clientState := session.status, session.clientState
	session.mu.Unlock()
	req := new(presencev1.RefreshUserSessionRequest)
	req.SetUserId(session.userID)
	req.SetSessionId(session.id)
	req.SetGatewayId(session.gatewayID)
	req.SetGeneration(session.gatewayGeneration)
	req.SetDeviceType(session.deviceType)
	req.SetStatus(statusValue)
	req.SetClientState(clientState)
	req.SetGuildIds(presenceGuildIDs(session))
	return req
}

func (s *Server) refreshAllRoutes(ctx context.Context) {
	routes := s.routeSnapshot()
	active := make(map[store.Route]struct{}, len(routes))
	for _, route := range routes {
		active[route] = struct{}{}
	}

	s.routeMu.Lock()
	defer s.routeMu.Unlock()

	detached := make([]store.Route, 0)
	for route := range s.publishedRoutes {
		if _, ok := active[route]; !ok {
			detached = append(detached, route)
		}
	}
	if err := s.svcCtx.Store.DetachRoutes(ctx, s.nodeID, s.generation, detached); err != nil {
		if ctx.Err() == nil {
			logx.WithContext(ctx).Errorw("detach session routes", logx.Field("error", err))
		}
	} else {
		for _, route := range detached {
			delete(s.publishedRoutes, route)
		}
	}

	if err := s.svcCtx.Store.RefreshRoutes(ctx, s.nodeID, s.generation, routes, s.svcCtx.Cfg.Node.RouteTTL()); err != nil && ctx.Err() == nil {
		logx.WithContext(ctx).Errorw("refresh session routes", logx.Field("error", err))
	} else if err == nil {
		for route := range active {
			s.publishedRoutes[route] = struct{}{}
		}
	}
}

func (s *Server) routeSnapshot() []store.Route {
	s.mu.RLock()
	defer s.mu.RUnlock()
	routes := make([]store.Route, 0, len(s.users)+len(s.guilds))
	for id := range s.users {
		routes = append(routes, store.Route{Kind: store.RouteUser, ID: id})
	}
	for id := range s.guilds {
		routes = append(routes, store.Route{Kind: store.RouteGuild, ID: id})
	}
	return routes
}

func (s *Server) cleanupDetached(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := time.Now()
		s.mu.RLock()
		sessions := make([]*logicalSession, 0, len(s.sessions))
		for _, session := range s.sessions {
			sessions = append(sessions, session)
		}
		s.mu.RUnlock()
		for _, session := range sessions {
			session.mu.Lock()
			expired := session.binding == nil && !session.detachedAt.IsZero() &&
				now.Sub(session.detachedAt) >= s.svcCtx.Cfg.Node.ResumeTTL()
			session.mu.Unlock()
			if expired {
				s.removeSession(ctx, session)
			}
		}
	}
}

func stringifyIDs(ids []int64) []string {
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = strconv.FormatInt(id, 10)
	}
	return values
}

func statusFromString(value string) presencev1.PresenceStatus {
	switch strings.ToLower(strings.TrimSpace(value)) {
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

func clientStateFromString(value string) presencev1.ClientState {
	if strings.EqualFold(strings.TrimSpace(value), "background") {
		return presencev1.ClientState_CLIENT_STATE_BACKGROUND
	}
	return presencev1.ClientState_CLIENT_STATE_FOREGROUND
}

func randomID(prefix string) string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(value[:])
}

func mapKeys(values map[int64]struct{}) []int64 {
	result := make([]int64, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	return result
}

func parseID(value string) int64 {
	id, _ := strconv.ParseInt(value, 10, 64)
	return id
}
