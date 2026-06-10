package server

import (
	"context"
	"errors"
	"testing"

	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	"github.com/soasurs/cordis/services/presence/v1/config"
	"github.com/soasurs/cordis/services/presence/v1/internal/store"
	"github.com/soasurs/cordis/services/presence/v1/internal/svc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRegisterGateway(t *testing.T) {
	server, fake := newTestServer()

	req := new(presencev1.RegisterGatewayRequest)
	req.SetGatewayId("gw-a")
	req.SetGeneration("gen-1")
	req.SetRpcAddr("10.0.0.1:3004")

	resp, err := server.RegisterGateway(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, "gw-a", resp.GetGateway().GetGatewayId())
	require.Equal(t, "gen-1", resp.GetGateway().GetGeneration())
	require.Equal(t, "10.0.0.1:3004", resp.GetGateway().GetRpcAddr())
	require.Equal(t, int64(12345), resp.GetGateway().GetExpiresAt())
	require.Equal(t, store.Gateway{
		GatewayID:  "gw-a",
		Generation: "gen-1",
		RPCAddr:    "10.0.0.1:3004",
	}, fake.upserted)
}

func TestRegisterGatewayValidation(t *testing.T) {
	server, _ := newTestServer()

	_, err := server.RegisterGateway(t.Context(), new(presencev1.RegisterGatewayRequest))
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestRefreshChannelRoutes(t *testing.T) {
	server, fake := newTestServer()

	req := new(presencev1.RefreshChannelRoutesRequest)
	req.SetGatewayId("gw-a")
	req.SetGeneration("gen-1")
	req.SetChannelIds([]int64{1001, 1002})

	resp, err := server.RefreshChannelRoutes(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int32(2), resp.GetRefreshed())
	require.Equal(t, "gw-a", fake.refreshedGatewayID)
	require.Equal(t, "gen-1", fake.refreshedGeneration)
	require.Equal(t, []int64{1001, 1002}, fake.refreshedChannelIDs)
}

func TestDetachChannelRoute(t *testing.T) {
	server, fake := newTestServer()

	req := new(presencev1.DetachChannelRouteRequest)
	req.SetGatewayId("gw-a")
	req.SetGeneration("gen-1")
	req.SetChannelId(1001)

	resp, err := server.DetachChannelRoute(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Equal(t, "gw-a", fake.detachedGatewayID)
	require.Equal(t, "gen-1", fake.detachedGeneration)
	require.Equal(t, int64(1001), fake.detachedChannelID)
}

func TestResolveChannelGateways(t *testing.T) {
	server, fake := newTestServer()
	fake.gateways = []store.Gateway{
		{GatewayID: "gw-a", Generation: "gen-1", RPCAddr: "10.0.0.1:3004", ExpiresAt: 1000},
		{GatewayID: "gw-b", Generation: "gen-2", RPCAddr: "10.0.0.2:3004", ExpiresAt: 2000},
	}

	req := new(presencev1.ResolveChannelGatewaysRequest)
	req.SetChannelId(1001)

	resp, err := server.ResolveChannelGateways(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), fake.resolvedChannelID)
	require.Len(t, resp.GetGateways(), 2)
	require.Equal(t, "gw-a", resp.GetGateways()[0].GetGatewayId())
	require.Equal(t, "gen-2", resp.GetGateways()[1].GetGeneration())
}

func TestRegisterUserSession(t *testing.T) {
	server, fake := newTestServer()

	req := new(presencev1.RegisterUserSessionRequest)
	req.SetUserId(1001)
	req.SetSessionId("sess-a")
	req.SetGatewayId("gw-a")
	req.SetGeneration("gen-1")
	req.SetDeviceType("desktop")
	req.SetStatus(presencev1.PresenceStatus_PRESENCE_STATUS_DND)
	req.SetClientState(presencev1.ClientState_CLIENT_STATE_BACKGROUND)

	resp, err := server.RegisterUserSession(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, presencev1.PresenceStatus_PRESENCE_STATUS_DND, resp.GetPresence().GetStatus())
	require.Equal(t, store.UserSession{
		UserID:      1001,
		SessionID:   "sess-a",
		GatewayID:   "gw-a",
		Generation:  "gen-1",
		DeviceType:  "desktop",
		Status:      store.PresenceStatusDND,
		ClientState: store.ClientStateBackground,
	}, fake.upsertedSession)
}

func TestRegisterUserSessionValidation(t *testing.T) {
	server, _ := newTestServer()

	_, err := server.RegisterUserSession(t.Context(), new(presencev1.RegisterUserSessionRequest))
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestUpdateUserPresence(t *testing.T) {
	server, fake := newTestServer()

	req := new(presencev1.UpdateUserPresenceRequest)
	req.SetUserId(1001)
	req.SetSessionId("sess-a")
	req.SetStatus(presencev1.PresenceStatus_PRESENCE_STATUS_IDLE)
	req.SetClientState(presencev1.ClientState_CLIENT_STATE_BACKGROUND)

	resp, err := server.UpdateUserPresence(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, presencev1.PresenceStatus_PRESENCE_STATUS_IDLE, resp.GetPresence().GetStatus())
	require.Equal(t, store.UserSession{
		UserID:      1001,
		SessionID:   "sess-a",
		Status:      store.PresenceStatusIdle,
		ClientState: store.ClientStateBackground,
	}, fake.updatedSession)
}

func TestRemoveUserSession(t *testing.T) {
	server, fake := newTestServer()

	req := new(presencev1.RemoveUserSessionRequest)
	req.SetUserId(1001)
	req.SetSessionId("sess-a")

	resp, err := server.RemoveUserSession(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Equal(t, int64(1001), fake.removedUserID)
	require.Equal(t, "sess-a", fake.removedSessionID)
}

func TestResolveUsersPresence(t *testing.T) {
	server, fake := newTestServer()
	fake.presences = []store.UserPresence{
		{
			UserID:     1001,
			Status:     store.PresenceStatusOnline,
			LastSeenAt: 123,
			Sessions: []store.UserSession{
				{
					UserID:      1001,
					SessionID:   "sess-a",
					GatewayID:   "gw-a",
					Generation:  "gen-1",
					DeviceType:  "desktop",
					Status:      store.PresenceStatusOnline,
					ClientState: store.ClientStateForeground,
					LastSeenAt:  123,
					ExpiresAt:   456,
				},
			},
		},
		{UserID: 1002, Status: store.PresenceStatusOffline},
	}

	req := new(presencev1.ResolveUsersPresenceRequest)
	req.SetUserIds([]int64{1001, 1002})

	resp, err := server.ResolveUsersPresence(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []int64{1001, 1002}, fake.resolvedUserIDs)
	require.Len(t, resp.GetPresences(), 2)
	require.Equal(t, presencev1.PresenceStatus_PRESENCE_STATUS_ONLINE, resp.GetPresences()[0].GetStatus())
	require.Len(t, resp.GetPresences()[0].GetSessions(), 1)
	require.Equal(t, presencev1.PresenceStatus_PRESENCE_STATUS_OFFLINE, resp.GetPresences()[1].GetStatus())
}

func TestStoreErrorPropagates(t *testing.T) {
	server, fake := newTestServer()
	fake.err = errors.New("redis unavailable")

	req := new(presencev1.ResolveChannelGatewaysRequest)
	req.SetChannelId(1001)

	_, err := server.ResolveChannelGateways(t.Context(), req)
	require.ErrorIs(t, err, fake.err)
}

func newTestServer() (presencev1.PresenceServiceServer, *fakeStore) {
	fake := &fakeStore{}
	svcCtx := svc.NewServiceContextWithDependencies(config.Config{}, svc.Dependencies{Store: fake})
	return New(svcCtx), fake
}

type fakeStore struct {
	err error

	upserted store.Gateway

	refreshedGatewayID  string
	refreshedGeneration string
	refreshedChannelIDs []int64

	detachedGatewayID  string
	detachedGeneration string
	detachedChannelID  int64

	resolvedChannelID int64
	gateways          []store.Gateway

	upsertedSession  store.UserSession
	updatedSession   store.UserSession
	removedUserID    int64
	removedSessionID string
	resolvedUserIDs  []int64
	presences        []store.UserPresence
}

func (s *fakeStore) UpsertGateway(_ context.Context, gateway store.Gateway) (store.Gateway, error) {
	if s.err != nil {
		return store.Gateway{}, s.err
	}
	s.upserted = gateway
	gateway.ExpiresAt = 12345
	return gateway, nil
}

func (s *fakeStore) RefreshChannelRoutes(_ context.Context, gatewayID, generation string, channelIDs []int64) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	s.refreshedGatewayID = gatewayID
	s.refreshedGeneration = generation
	s.refreshedChannelIDs = append([]int64(nil), channelIDs...)
	return len(channelIDs), nil
}

func (s *fakeStore) DetachChannelRoute(_ context.Context, gatewayID, generation string, channelID int64) error {
	if s.err != nil {
		return s.err
	}
	s.detachedGatewayID = gatewayID
	s.detachedGeneration = generation
	s.detachedChannelID = channelID
	return nil
}

func (s *fakeStore) ResolveChannelGateways(_ context.Context, channelID int64) ([]store.Gateway, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.resolvedChannelID = channelID
	return s.gateways, nil
}

func (s *fakeStore) UpsertUserSession(_ context.Context, session store.UserSession) (store.UserPresence, error) {
	if s.err != nil {
		return store.UserPresence{}, s.err
	}
	s.upsertedSession = session
	return store.UserPresence{
		UserID: session.UserID,
		Status: session.Status,
		Sessions: []store.UserSession{
			session,
		},
	}, nil
}

func (s *fakeStore) UpdateUserSession(_ context.Context, session store.UserSession) (store.UserPresence, error) {
	if s.err != nil {
		return store.UserPresence{}, s.err
	}
	s.updatedSession = session
	return store.UserPresence{
		UserID: session.UserID,
		Status: session.Status,
		Sessions: []store.UserSession{
			session,
		},
	}, nil
}

func (s *fakeStore) RemoveUserSession(_ context.Context, userID int64, sessionID string) error {
	if s.err != nil {
		return s.err
	}
	s.removedUserID = userID
	s.removedSessionID = sessionID
	return nil
}

func (s *fakeStore) ResolveUsersPresence(_ context.Context, userIDs []int64) ([]store.UserPresence, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.resolvedUserIDs = append([]int64(nil), userIDs...)
	return s.presences, nil
}
