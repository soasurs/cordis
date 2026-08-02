package server

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/observability"
	"github.com/soasurs/cordis/pkg/realtime"
	sessionratelimit "github.com/soasurs/cordis/services/session/v1/ratelimit"
)

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

func (s *Server) identify(
	ctx context.Context,
	connectionID, gatewayID, gatewayGeneration string,
	data *sessionv1.Identify,
) (*logicalSession, error) {
	initialStatus, hasInitialStatus, clientState, err := identifyPresence(data)
	if err != nil {
		return nil, err
	}
	auth, err := s.authenticateGatewayCredential(ctx, data.GetToken(), data.GetGatewayTicket())
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
		initialStatus:     initialStatus,
		hasInitialStatus:  hasInitialStatus,
		clientState:       clientState,
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
	preference, err := s.registerPresence(ctx, session)
	if err != nil {
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
	profiles, err := s.getReadyUserProfiles(ctx, session.userID, messageReady.GetDmChannels())
	if err != nil {
		s.removeSession(ctx, session)
		return nil, err
	}
	presences, err := s.getReadyPresences(ctx, session.userID, profiles)
	if err != nil {
		s.removeSession(ctx, session)
		return nil, err
	}
	ready, err := marshalReady(
		session, auth.GetExpiresAt(), readyGuilds, messageReady, profiles, presences, preference, s.nodeID,
	)
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
	if (strings.TrimSpace(data.GetToken()) == "" && strings.TrimSpace(data.GetGatewayTicket()) == "") || strings.TrimSpace(data.GetSessionId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "credential and session id are required")
	}
	s.mu.RLock()
	session := s.sessions[data.GetSessionId()]
	s.mu.RUnlock()
	if session == nil {
		return nil, status.Error(codes.NotFound, "session not found")
	}

	auth, err := s.authenticateGatewayCredential(ctx, data.GetToken(), data.GetGatewayTicket())
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

func (s *Server) authenticateGatewayCredential(ctx context.Context, accessToken, gatewayTicket string) (*authenticatorv1.VerifyAccessTokenResponse, error) {
	hasAccessToken := strings.TrimSpace(accessToken) != ""
	hasGatewayTicket := strings.TrimSpace(gatewayTicket) != ""
	if hasAccessToken == hasGatewayTicket {
		return nil, status.Error(codes.Unauthenticated, "exactly one gateway credential is required")
	}
	if hasAccessToken {
		req := new(authenticatorv1.VerifyAccessTokenRequest)
		req.SetAccessToken(accessToken)
		return s.svcCtx.AuthenticatorClient.VerifyAccessToken(ctx, req)
	}
	req := new(authenticatorv1.RedeemGatewayTicketRequest)
	req.SetGatewayTicket(gatewayTicket)
	redeemed, err := s.svcCtx.AuthenticatorClient.RedeemGatewayTicket(ctx, req)
	if err != nil {
		return nil, err
	}
	if !redeemed.GetOk() || redeemed.GetUserId() <= 0 || redeemed.GetSessionId() <= 0 {
		return nil, status.Error(codes.Unauthenticated, "invalid gateway ticket")
	}
	resp := new(authenticatorv1.VerifyAccessTokenResponse)
	resp.SetOk(redeemed.GetOk())
	resp.SetUserId(redeemed.GetUserId())
	resp.SetSessionId(redeemed.GetSessionId())
	resp.SetExpiresAt(redeemed.GetAccessTokenExpiresAt())
	return resp, nil
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
