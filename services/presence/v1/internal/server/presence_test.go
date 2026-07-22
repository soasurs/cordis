package server

import (
	"context"
	"errors"
	"sync"
	"testing"

	sn "github.com/bwmarrin/snowflake"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	"github.com/soasurs/cordis/services/presence/v1/config"
	"github.com/soasurs/cordis/services/presence/v1/internal/store"
	"github.com/soasurs/cordis/services/presence/v1/internal/svc"
)

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

func TestRefreshUserSessions(t *testing.T) {
	server, fake := newTestServer()
	fake.missingSessionIDs = []string{"sess-b"}
	req := new(presencev1.RefreshUserSessionsRequest)
	items := make([]*presencev1.RefreshUserSessionRequest, 0, 2)
	for _, sessionID := range []string{"sess-a", "sess-b"} {
		item := new(presencev1.RefreshUserSessionRequest)
		item.SetUserId(1001)
		item.SetSessionId(sessionID)
		item.SetGatewayId("gw-a")
		item.SetGeneration("gen-1")
		items = append(items, item)
	}
	req.SetSessions(items)

	resp, err := server.RefreshUserSessions(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []string{"sess-b"}, resp.GetMissingSessionIds())
	require.Len(t, fake.refreshedSessions, 2)
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

	req := new(presencev1.ResolveUsersPresenceRequest)
	req.SetUserIds([]int64{1001})

	_, err := server.ResolveUsersPresence(t.Context(), req)
	require.ErrorIs(t, err, fake.err)
}

func newTestServer() (presencev1.PresenceServiceServer, *fakeStore) {
	fake := &fakeStore{}
	svcCtx := svc.NewServiceContextWithDependencies(config.Config{}, svc.Dependencies{Store: fake, Snowflake: newTestSnowflake()})
	return New(svcCtx), fake
}

func newTestSnowflake() *sn.Node {
	node, err := sn.NewNode(1)
	if err != nil {
		panic(err)
	}
	return node
}

type fakeStore struct {
	err error
	mu  sync.Mutex

	upsertedSession  store.UserSession
	updatedSession   store.UserSession
	removedUserID    int64
	removedSessionID string
	resolvedUserIDs  []int64
	presences        []store.UserPresence
	// presenceSequence, when non-empty, feeds successive ResolveUsersPresence
	// calls before falling back to presences.
	presenceSequence  [][]store.UserPresence
	missingSessionIDs []string
	refreshedSessions []store.UserSession
}

func (s *fakeStore) WithUserMutation(ctx context.Context, _ int64, fn func(context.Context) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(ctx)
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

func (s *fakeStore) RefreshUserSessions(_ context.Context, sessions []store.UserSession) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.refreshedSessions = append([]store.UserSession(nil), sessions...)
	return append([]string(nil), s.missingSessionIDs...), nil
}

func (s *fakeStore) UpdateUserSession(_ context.Context, session store.UserSession) (store.UserPresence, error) {
	if s.err != nil {
		return store.UserPresence{}, s.err
	}
	s.updatedSession = session
	s.presences = []store.UserPresence{{UserID: session.UserID, Status: session.Status}}
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
	if len(s.presenceSequence) > 0 {
		next := s.presenceSequence[0]
		s.presenceSequence = s.presenceSequence[1:]
		return next, nil
	}
	return s.presences, nil
}
