package server

import (
	"context"

	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	"github.com/soasurs/cordis/services/presence/v1/internal/store"
)

func (s *presenceServer) RegisterGateway(ctx context.Context, req *presencev1.RegisterGatewayRequest) (*presencev1.RegisterGatewayResponse, error) {
	if req.GetGatewayId() == "" {
		return nil, errGatewayIDRequired
	}
	if req.GetGeneration() == "" {
		return nil, errGenerationRequired
	}
	if req.GetRpcAddr() == "" {
		return nil, errRPCAddrRequired
	}

	gateway, err := s.svcCtx.Store.UpsertGateway(ctx, store.Gateway{
		GatewayID:  req.GetGatewayId(),
		Generation: req.GetGeneration(),
		RPCAddr:    req.GetRpcAddr(),
	})
	if err != nil {
		return nil, err
	}

	resp := new(presencev1.RegisterGatewayResponse)
	resp.SetGateway(gatewayToProto(gateway))
	return resp, nil
}

func (s *presenceServer) RefreshChannelRoutes(ctx context.Context, req *presencev1.RefreshChannelRoutesRequest) (*presencev1.RefreshChannelRoutesResponse, error) {
	if req.GetGatewayId() == "" {
		return nil, errGatewayIDRequired
	}
	if req.GetGeneration() == "" {
		return nil, errGenerationRequired
	}

	refreshed, err := s.svcCtx.Store.RefreshChannelRoutes(ctx, req.GetGatewayId(), req.GetGeneration(), req.GetChannelIds())
	if err != nil {
		return nil, err
	}

	resp := new(presencev1.RefreshChannelRoutesResponse)
	resp.SetRefreshed(int32(refreshed))
	return resp, nil
}

func (s *presenceServer) DetachChannelRoute(ctx context.Context, req *presencev1.DetachChannelRouteRequest) (*presencev1.DetachChannelRouteResponse, error) {
	if req.GetGatewayId() == "" {
		return nil, errGatewayIDRequired
	}
	if req.GetGeneration() == "" {
		return nil, errGenerationRequired
	}
	if req.GetChannelId() == 0 {
		return nil, errChannelIDRequired
	}

	if err := s.svcCtx.Store.DetachChannelRoute(ctx, req.GetGatewayId(), req.GetGeneration(), req.GetChannelId()); err != nil {
		return nil, err
	}

	resp := new(presencev1.DetachChannelRouteResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *presenceServer) ResolveChannelGateways(ctx context.Context, req *presencev1.ResolveChannelGatewaysRequest) (*presencev1.ResolveChannelGatewaysResponse, error) {
	if req.GetChannelId() == 0 {
		return nil, errChannelIDRequired
	}

	gateways, err := s.svcCtx.Store.ResolveChannelGateways(ctx, req.GetChannelId())
	if err != nil {
		return nil, err
	}

	resp := new(presencev1.ResolveChannelGatewaysResponse)
	resp.SetGateways(gatewaysToProto(gateways))
	return resp, nil
}

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

func gatewayToProto(gateway store.Gateway) *presencev1.GatewayInstance {
	msg := new(presencev1.GatewayInstance)
	msg.SetGatewayId(gateway.GatewayID)
	msg.SetGeneration(gateway.Generation)
	msg.SetRpcAddr(gateway.RPCAddr)
	msg.SetExpiresAt(gateway.ExpiresAt)
	return msg
}

func gatewaysToProto(gateways []store.Gateway) []*presencev1.GatewayInstance {
	values := make([]*presencev1.GatewayInstance, 0, len(gateways))
	for _, gateway := range gateways {
		values = append(values, gatewayToProto(gateway))
	}
	return values
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
