package server

import (
	"context"
	"strconv"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	sessionratelimit "github.com/soasurs/cordis/services/session/v1/ratelimit"
)

func (s *Server) updatePresence(ctx context.Context, session *logicalSession, binding *binding, data *sessionv1.PresenceUpdate) error {
	if !data.HasStatus() && !data.HasClientState() {
		return status.Error(codes.InvalidArgument, "presence update must include status or client_state")
	}
	var (
		requestedStatus      presencev1.PresenceStatus
		requestedClientState presencev1.ClientState
		err                  error
	)
	if data.HasStatus() {
		requestedStatus, err = parsePresenceStatus(data.GetStatus())
		if err != nil {
			return err
		}
	}
	if data.HasClientState() {
		requestedClientState, err = parseClientState(data.GetClientState())
		if err != nil {
			return err
		}
	}
	now := time.Now()

	session.mu.Lock()
	if session.binding != binding {
		session.mu.Unlock()
		return status.Error(codes.Aborted, "stale session binding")
	}
	clientState := session.clientState
	if data.HasClientState() {
		clientState = requestedClientState
	}
	if !data.HasStatus() && session.clientState == clientState {
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
	clientState = session.clientState
	if data.HasClientState() {
		clientState = requestedClientState
	}
	if !data.HasStatus() && session.clientState == clientState {
		session.mu.Unlock()
		return nil
	}
	oldClientState := session.clientState
	session.clientState = clientState
	session.mu.Unlock()

	if err := s.updatePresenceRPC(ctx, session, data.HasStatus(), requestedStatus, data.HasClientState()); err != nil {
		session.mu.Lock()
		if session.clientState == clientState {
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

// presenceGuildIDs snapshots the session's guild memberships for presence
// transition fan-out.
func presenceGuildIDs(session *logicalSession) []int64 {
	session.mu.Lock()
	defer session.mu.Unlock()
	return mapKeys(session.guilds)
}

func (s *Server) registerPresence(
	ctx context.Context,
	session *logicalSession,
) (*presencev1.UserPresencePreference, error) {
	req := new(presencev1.RegisterUserSessionRequest)
	req.SetUserId(session.userID)
	req.SetSessionId(session.id)
	req.SetGatewayId(session.gatewayID)
	req.SetGeneration(session.gatewayGeneration)
	req.SetDeviceType(session.deviceType)
	if session.hasInitialStatus {
		req.SetInitialStatus(session.initialStatus)
	}
	req.SetClientState(session.clientState)
	req.SetGuildIds(presenceGuildIDs(session))
	resp, err := s.svcCtx.PresenceClient.RegisterUserSession(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.GetPreference() == nil || resp.GetPreference().GetVersion() <= 0 {
		return nil, status.Error(codes.Internal, "presence service returned an invalid preference")
	}
	return resp.GetPreference(), nil
}

func (s *Server) refreshPresence(ctx context.Context, session *logicalSession) error {
	session.mu.Lock()
	clientState := session.clientState
	session.mu.Unlock()
	req := new(presencev1.RefreshUserSessionRequest)
	req.SetUserId(session.userID)
	req.SetSessionId(session.id)
	req.SetGatewayId(session.gatewayID)
	req.SetGeneration(session.gatewayGeneration)
	req.SetDeviceType(session.deviceType)
	req.SetClientState(clientState)
	req.SetGuildIds(presenceGuildIDs(session))
	_, err := s.svcCtx.PresenceClient.RefreshUserSession(ctx, req)
	return err
}

func (s *Server) updatePresenceRPC(
	ctx context.Context,
	session *logicalSession,
	hasStatus bool,
	statusValue presencev1.PresenceStatus,
	hasClientState bool,
) error {
	session.mu.Lock()
	clientState := session.clientState
	session.mu.Unlock()
	req := new(presencev1.UpdateUserPresenceRequest)
	req.SetUserId(session.userID)
	req.SetSessionId(session.id)
	if hasStatus {
		req.SetStatus(statusValue)
	}
	if hasClientState {
		req.SetClientState(clientState)
	}
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

func identifyPresence(
	data *sessionv1.Identify,
) (presencev1.PresenceStatus, bool, presencev1.ClientState, error) {
	var statusValue presencev1.PresenceStatus
	hasStatus := data.HasStatus()
	clientState := presencev1.ClientState_CLIENT_STATE_FOREGROUND
	var err error
	if hasStatus {
		statusValue, err = parsePresenceStatus(data.GetStatus())
		if err != nil {
			return 0, false, 0, err
		}
	}
	if data.HasClientState() {
		clientState, err = parseClientState(data.GetClientState())
		if err != nil {
			return 0, false, 0, err
		}
	}
	return statusValue, hasStatus, clientState, nil
}

func parsePresenceStatus(value string) (presencev1.PresenceStatus, error) {
	switch value {
	case "online":
		return presencev1.PresenceStatus_PRESENCE_STATUS_ONLINE, nil
	case "idle":
		return presencev1.PresenceStatus_PRESENCE_STATUS_IDLE, nil
	case "dnd":
		return presencev1.PresenceStatus_PRESENCE_STATUS_DND, nil
	case "invisible":
		return presencev1.PresenceStatus_PRESENCE_STATUS_INVISIBLE, nil
	case "offline":
		return 0, status.Error(codes.InvalidArgument, "status cannot be offline")
	default:
		return 0, status.Error(codes.InvalidArgument, "status is invalid")
	}
}

func parseClientState(value string) (presencev1.ClientState, error) {
	switch value {
	case "foreground":
		return presencev1.ClientState_CLIENT_STATE_FOREGROUND, nil
	case "background":
		return presencev1.ClientState_CLIENT_STATE_BACKGROUND, nil
	default:
		return 0, status.Error(codes.InvalidArgument, "client_state is invalid")
	}
}
