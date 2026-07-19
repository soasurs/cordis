package server

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	"github.com/soasurs/cordis/services/presence/v1/internal/store"
)

func (s *presenceServer) RegisterUserSession(ctx context.Context, req *presencev1.RegisterUserSessionRequest) (*presencev1.RegisterUserSessionResponse, error) {
	if err := validateUserSessionRequest(req.GetUserId(), req.GetSessionId(), req.GetGatewayId(), req.GetGeneration()); err != nil {
		return nil, err
	}

	presence, err := s.mutateUserPresence(ctx, req.GetUserId(), req.GetGuildIds(), func(ctx context.Context) (store.UserPresence, error) {
		return s.svcCtx.Store.UpsertUserSession(ctx, store.UserSession{
			UserID:      req.GetUserId(),
			SessionID:   req.GetSessionId(),
			GatewayID:   req.GetGatewayId(),
			Generation:  req.GetGeneration(),
			DeviceType:  req.GetDeviceType(),
			Status:      protoStatusToStore(req.GetStatus()),
			ClientState: protoClientStateToStore(req.GetClientState()),
		})
	})
	if err != nil {
		return nil, err
	}

	resp := new(presencev1.RegisterUserSessionResponse)
	resp.SetPresence(userPresenceToProto(presence))
	return resp, nil
}

func (s *presenceServer) RefreshUserSession(ctx context.Context, req *presencev1.RefreshUserSessionRequest) (*presencev1.RefreshUserSessionResponse, error) {
	if err := validateUserSessionRequest(req.GetUserId(), req.GetSessionId(), req.GetGatewayId(), req.GetGeneration()); err != nil {
		return nil, err
	}

	presence, err := s.mutateUserPresence(ctx, req.GetUserId(), req.GetGuildIds(), func(ctx context.Context) (store.UserPresence, error) {
		return s.svcCtx.Store.UpsertUserSession(ctx, store.UserSession{
			UserID:      req.GetUserId(),
			SessionID:   req.GetSessionId(),
			GatewayID:   req.GetGatewayId(),
			Generation:  req.GetGeneration(),
			DeviceType:  req.GetDeviceType(),
			Status:      protoStatusToStore(req.GetStatus()),
			ClientState: protoClientStateToStore(req.GetClientState()),
		})
	})
	if err != nil {
		return nil, err
	}

	resp := new(presencev1.RefreshUserSessionResponse)
	resp.SetPresence(userPresenceToProto(presence))
	return resp, nil
}

func (s *presenceServer) RefreshUserSessions(ctx context.Context, req *presencev1.RefreshUserSessionsRequest) (*presencev1.RefreshUserSessionsResponse, error) {
	if len(req.GetSessions()) == 0 {
		return new(presencev1.RefreshUserSessionsResponse), nil
	}
	if len(req.GetSessions()) > 500 {
		return nil, status.Error(codes.InvalidArgument, "too many sessions")
	}
	sessions := make([]store.UserSession, 0, len(req.GetSessions()))
	seen := make(map[string]struct{}, len(req.GetSessions()))
	for _, item := range req.GetSessions() {
		if err := validateUserSessionRequest(item.GetUserId(), item.GetSessionId(), item.GetGatewayId(), item.GetGeneration()); err != nil {
			return nil, err
		}
		if _, ok := seen[item.GetSessionId()]; ok {
			return nil, status.Error(codes.InvalidArgument, "session id must be unique")
		}
		seen[item.GetSessionId()] = struct{}{}
		sessions = append(sessions, store.UserSession{
			UserID: item.GetUserId(), SessionID: item.GetSessionId(), GatewayID: item.GetGatewayId(),
			Generation: item.GetGeneration(), DeviceType: item.GetDeviceType(),
			Status: protoStatusToStore(item.GetStatus()), ClientState: protoClientStateToStore(item.GetClientState()),
		})
	}
	missing, err := s.svcCtx.Store.RefreshUserSessions(ctx, sessions)
	if err != nil {
		return nil, err
	}
	resp := new(presencev1.RefreshUserSessionsResponse)
	resp.SetMissingSessionIds(missing)
	return resp, nil
}

func (s *presenceServer) UpdateUserPresence(ctx context.Context, req *presencev1.UpdateUserPresenceRequest) (*presencev1.UpdateUserPresenceResponse, error) {
	if req.GetUserId() == 0 {
		return nil, errUserIDRequired
	}
	if req.GetSessionId() == "" {
		return nil, errSessionIDRequired
	}

	presence, err := s.mutateUserPresence(ctx, req.GetUserId(), req.GetGuildIds(), func(ctx context.Context) (store.UserPresence, error) {
		return s.svcCtx.Store.UpdateUserSession(ctx, store.UserSession{
			UserID:      req.GetUserId(),
			SessionID:   req.GetSessionId(),
			Status:      protoStatusToStore(req.GetStatus()),
			ClientState: protoClientStateToStore(req.GetClientState()),
		})
	})
	if err != nil {
		return nil, err
	}

	resp := new(presencev1.UpdateUserPresenceResponse)
	resp.SetPresence(userPresenceToProto(presence))
	return resp, nil
}

func (s *presenceServer) RemoveUserSession(ctx context.Context, req *presencev1.RemoveUserSessionRequest) (*presencev1.RemoveUserSessionResponse, error) {
	if req.GetUserId() == 0 {
		return nil, errUserIDRequired
	}
	if req.GetSessionId() == "" {
		return nil, errSessionIDRequired
	}

	err := s.svcCtx.Store.WithUserMutation(ctx, req.GetUserId(), func(ctx context.Context) error {
		oldStatus, known := s.previousStatus(ctx, req.GetUserId())
		if err := s.svcCtx.Store.RemoveUserSession(ctx, req.GetUserId(), req.GetSessionId()); err != nil {
			return err
		}
		if known {
			if presences, err := s.svcCtx.Store.ResolveUsersPresence(ctx, []int64{req.GetUserId()}); err == nil && len(presences) == 1 {
				s.publishTransition(ctx, req.GetUserId(), req.GetGuildIds(), oldStatus, true, presences[0].Status)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	resp := new(presencev1.RemoveUserSessionResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *presenceServer) mutateUserPresence(
	ctx context.Context,
	userID int64,
	guildIDs []int64,
	mutate func(context.Context) (store.UserPresence, error),
) (store.UserPresence, error) {
	var presence store.UserPresence
	err := s.svcCtx.Store.WithUserMutation(ctx, userID, func(ctx context.Context) error {
		oldStatus, known := s.previousStatus(ctx, userID)
		var err error
		presence, err = mutate(ctx)
		if err != nil {
			return err
		}
		s.publishTransition(ctx, userID, guildIDs, oldStatus, known, presence.Status)
		return nil
	})
	return presence, err
}

func (s *presenceServer) ResolveUsersPresence(ctx context.Context, req *presencev1.ResolveUsersPresenceRequest) (*presencev1.ResolveUsersPresenceResponse, error) {
	presences, err := s.svcCtx.Store.ResolveUsersPresence(ctx, req.GetUserIds())
	if err != nil {
		return nil, err
	}

	resp := new(presencev1.ResolveUsersPresenceResponse)
	resp.SetPresences(userPresencesToProto(presences))
	return resp, nil
}

func validateUserSessionRequest(userID int64, sessionID, gatewayID, generation string) error {
	if userID == 0 {
		return errUserIDRequired
	}
	if sessionID == "" {
		return errSessionIDRequired
	}
	if gatewayID == "" {
		return errGatewayIDRequired
	}
	if generation == "" {
		return errGenerationRequired
	}
	return nil
}

func userSessionToProto(session store.UserSession) *presencev1.UserSession {
	msg := new(presencev1.UserSession)
	msg.SetUserId(session.UserID)
	msg.SetSessionId(session.SessionID)
	msg.SetGatewayId(session.GatewayID)
	msg.SetGeneration(session.Generation)
	msg.SetDeviceType(session.DeviceType)
	msg.SetStatus(storeStatusToProto(session.Status))
	msg.SetClientState(storeClientStateToProto(session.ClientState))
	msg.SetLastSeenAt(session.LastSeenAt)
	msg.SetExpiresAt(session.ExpiresAt)
	return msg
}

func userPresenceToProto(presence store.UserPresence) *presencev1.UserPresence {
	msg := new(presencev1.UserPresence)
	msg.SetUserId(presence.UserID)
	msg.SetStatus(storeStatusToProto(presence.Status))
	msg.SetLastSeenAt(presence.LastSeenAt)
	sessions := make([]*presencev1.UserSession, 0, len(presence.Sessions))
	for _, session := range presence.Sessions {
		sessions = append(sessions, userSessionToProto(session))
	}
	msg.SetSessions(sessions)
	return msg
}

func userPresencesToProto(presences []store.UserPresence) []*presencev1.UserPresence {
	values := make([]*presencev1.UserPresence, 0, len(presences))
	for _, presence := range presences {
		values = append(values, userPresenceToProto(presence))
	}
	return values
}

func protoStatusToStore(status presencev1.PresenceStatus) store.PresenceStatus {
	switch status {
	case presencev1.PresenceStatus_PRESENCE_STATUS_IDLE:
		return store.PresenceStatusIdle
	case presencev1.PresenceStatus_PRESENCE_STATUS_DND:
		return store.PresenceStatusDND
	case presencev1.PresenceStatus_PRESENCE_STATUS_INVISIBLE:
		return store.PresenceStatusInvisible
	default:
		return store.PresenceStatusOnline
	}
}

func storeStatusToProto(status store.PresenceStatus) presencev1.PresenceStatus {
	switch status {
	case store.PresenceStatusOffline:
		return presencev1.PresenceStatus_PRESENCE_STATUS_OFFLINE
	case store.PresenceStatusIdle:
		return presencev1.PresenceStatus_PRESENCE_STATUS_IDLE
	case store.PresenceStatusDND:
		return presencev1.PresenceStatus_PRESENCE_STATUS_DND
	case store.PresenceStatusInvisible:
		return presencev1.PresenceStatus_PRESENCE_STATUS_INVISIBLE
	default:
		return presencev1.PresenceStatus_PRESENCE_STATUS_ONLINE
	}
}

func protoClientStateToStore(state presencev1.ClientState) store.ClientState {
	switch state {
	case presencev1.ClientState_CLIENT_STATE_BACKGROUND:
		return store.ClientStateBackground
	default:
		return store.ClientStateForeground
	}
}

func storeClientStateToProto(state store.ClientState) presencev1.ClientState {
	switch state {
	case store.ClientStateBackground:
		return presencev1.ClientState_CLIENT_STATE_BACKGROUND
	default:
		return presencev1.ClientState_CLIENT_STATE_FOREGROUND
	}
}
