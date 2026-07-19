package server

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	coreratelimit "github.com/soasurs/cordis/pkg/ratelimit"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/pkg/sessionregistry"
	"github.com/soasurs/cordis/services/session/v1/config"
	"github.com/soasurs/cordis/services/session/v1/internal/store"
	"github.com/soasurs/cordis/services/session/v1/internal/svc"
	sessionratelimit "github.com/soasurs/cordis/services/session/v1/ratelimit"
)

func TestIdentifyAllowsMultipleLogicalSessionsPerAuthSession(t *testing.T) {
	server := newTestServer()
	identify := new(sessionv1.Identify)
	identify.SetToken("token")

	first, err := server.identify(t.Context(), "conn-a", "gateway-a", "gen-a", identify)
	require.NoError(t, err)
	second, err := server.identify(t.Context(), "conn-b", "gateway-b", "gen-b", identify)
	require.NoError(t, err)
	require.NotEqual(t, first.id, second.id)
	require.Equal(t, first.authSessionID, second.authSessionID)
	require.Len(t, server.sessions, 2)
	require.Len(t, server.users[first.userID], 2)
}

func TestIdentifyRateLimitsValidatedUserAndAuthSession(t *testing.T) {
	server := newTestServer()
	limiter := &sessionFakeRateLimiter{}
	server.svcCtx.RateLimiter = limiter

	err := server.checkIdentifyRateLimits(t.Context(), 1001, 2002)

	require.NoError(t, err)
	require.Equal(t, []sessionRateCall{
		{policy: sessionratelimit.PolicyIdentifyUser, key: "1001", cost: 1},
		{policy: sessionratelimit.PolicyIdentifyAuthSession, key: "2002", cost: 1},
	}, limiter.calls)
}

func TestIdentifyAndResumeReplay(t *testing.T) {
	server := newTestServer()
	identify := new(sessionv1.Identify)
	identify.SetToken("token")
	session, err := server.identify(t.Context(), "conn-a", "gateway-a", "gen-a", identify)
	require.NoError(t, err)
	require.Equal(t, uint64(1), session.sequence)

	session.mu.Lock()
	firstBinding := session.binding
	server.appendDispatchLocked(session, realtime.EventMessageCreated, []byte(`{"id":"1"}`))
	server.appendDispatchLocked(session, realtime.EventMessageUpdated, []byte(`{"id":"1"}`))
	session.mu.Unlock()
	server.detach(session, firstBinding, true)

	resume := new(sessionv1.Resume)
	resume.SetToken("token")
	resume.SetSessionId(session.id)
	resume.SetSequence(1)
	resumed, err := server.resume(t.Context(), "conn-b", "gateway-b", "gen-b", resume)
	require.NoError(t, err)
	require.Same(t, session, resumed)

	resumed.mu.Lock()
	binding := resumed.binding
	resumed.mu.Unlock()
	frames := []*sessionv1.ConnectResponse{<-binding.send, <-binding.send, <-binding.send}
	require.Equal(t, []uint64{2, 3, 4}, []uint64{
		frames[0].GetSequence(), frames[1].GetSequence(), frames[2].GetSequence(),
	})
	require.Equal(t, realtime.GatewayEventResumed, frames[2].GetType())
}

func TestGatewayPayloadEncodesSnowflakeIDsAsStrings(t *testing.T) {
	server := newTestServer()
	identify := new(sessionv1.Identify)
	identify.SetToken("token")
	session, err := server.identify(t.Context(), "conn-a", "gateway-a", "gen-a", identify)
	require.NoError(t, err)

	var ready map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(session.replay[0].frame.GetJsonPayload()), &ready))
	require.Equal(t, `"1001"`, string(ready["user_id"]))
	require.Equal(t, `"2002"`, string(ready["auth_session_id"]))
	require.Equal(t, `3003`, string(ready["access_token_expires_at"]))
	require.JSONEq(t, `[]`, string(ready["guild_ids"]))

}

func TestPresenceDeduplicatesNoOpUpdates(t *testing.T) {
	server := newTestServer()
	identify := new(sessionv1.Identify)
	identify.SetToken("token")
	session, err := server.identify(t.Context(), "conn-presence-noop", "gateway-a", "gen-a", identify)
	require.NoError(t, err)

	limiter := &sessionFakeRateLimiter{}
	presence := &recordingPresence{}
	server.svcCtx.RateLimiter = limiter
	server.svcCtx.PresenceClient = presence
	session.mu.Lock()
	binding := session.binding
	session.mu.Unlock()
	update := new(sessionv1.PresenceUpdate)
	update.SetStatus("online")
	update.SetClientState("foreground")

	require.NoError(t, server.updatePresence(t.Context(), session, binding, update))
	require.Empty(t, limiter.calls)
	require.Empty(t, presence.updates)
}

func TestPresenceLimitsEachLogicalSession(t *testing.T) {
	server := newTestServer()
	identify := new(sessionv1.Identify)
	identify.SetToken("token")
	session, err := server.identify(t.Context(), "conn-presence-limit", "gateway-a", "gen-a", identify)
	require.NoError(t, err)

	limiter := &sessionFakeRateLimiter{}
	presence := &recordingPresence{}
	server.svcCtx.RateLimiter = limiter
	server.svcCtx.PresenceClient = presence
	session.mu.Lock()
	binding := session.binding
	session.mu.Unlock()
	for i := range 6 {
		update := new(sessionv1.PresenceUpdate)
		if i%2 == 0 {
			update.SetStatus("idle")
		} else {
			update.SetStatus("online")
		}
		update.SetClientState("foreground")
		err = server.updatePresence(t.Context(), session, binding, update)
		if i < 5 {
			require.NoError(t, err)
		} else {
			require.Equal(t, codes.ResourceExhausted, status.Code(err))
		}
	}
	require.Len(t, presence.updates, 5)
	require.Len(t, limiter.calls, 5)
}

func TestPresenceAppliesCrossDeviceUserQuota(t *testing.T) {
	server := newTestServer()
	identify := new(sessionv1.Identify)
	identify.SetToken("token")
	session, err := server.identify(t.Context(), "conn-presence-user", "gateway-a", "gen-a", identify)
	require.NoError(t, err)

	limiter := &sessionFakeRateLimiter{decisions: map[string]coreratelimit.Decision{
		sessionratelimit.PolicyPresenceUser: {Allowed: false},
	}}
	presence := &recordingPresence{}
	server.svcCtx.RateLimiter = limiter
	server.svcCtx.PresenceClient = presence
	session.mu.Lock()
	binding := session.binding
	session.mu.Unlock()
	update := new(sessionv1.PresenceUpdate)
	update.SetStatus("idle")
	update.SetClientState("foreground")

	err = server.updatePresence(t.Context(), session, binding, update)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.Equal(t, []sessionRateCall{{
		policy: sessionratelimit.PolicyPresenceUser, key: "1001", cost: 1,
	}}, limiter.calls)
	require.Empty(t, presence.updates)
}

func TestReplayWindowKeepsLatestEvents(t *testing.T) {
	server := newTestServer()
	server.svcCtx.Cfg.Node.MaxReplayEvents = 3
	session := &logicalSession{
		guilds: make(map[int64]struct{}),
	}
	for range 5 {
		server.appendDispatchLocked(session, realtime.EventMessageCreated, []byte(`{}`))
	}
	require.Equal(t, uint64(5), session.sequence)
	require.Equal(t, uint64(2), session.replayFloor)
	require.Len(t, session.replay, 3)
	require.Equal(t, uint64(3), session.replay[0].sequence)
}

func TestRefreshSessionLeasesBatchesStoreAndPresence(t *testing.T) {
	server := newTestServer()
	identify := new(sessionv1.Identify)
	identify.SetToken("token")
	session, err := server.identify(t.Context(), "conn-a", "gateway-a", "gen-a", identify)
	require.NoError(t, err)
	fakeStore := server.svcCtx.Store.(*fakeStore)
	presence := new(batchPresence)
	server.svcCtx.PresenceClient = presence

	server.refreshSessionLeases(t.Context())

	require.Equal(t, []store.Owner{{SessionID: session.id, NodeID: server.nodeID, Generation: server.generation}}, fakeStore.batchOwners)
	require.Equal(t, 120*time.Second, fakeStore.batchOwnerTTL)
	require.Len(t, presence.requests, 1)
	require.Len(t, presence.requests[0].GetSessions(), 1)
	require.Equal(t, session.id, presence.requests[0].GetSessions()[0].GetSessionId())
}

func TestJitterDurationStaysWithinLeaseWindow(t *testing.T) {
	base := time.Minute
	for range 100 {
		got := jitterDuration(base)
		require.GreaterOrEqual(t, got, 48*time.Second)
		require.LessOrEqual(t, got, 72*time.Second)
	}
}

func TestBatchRefreshOffsetStaysWithinAssignedSlot(t *testing.T) {
	const batchCount = 100
	spread := 5 * time.Second
	slot := spread / batchCount
	for batch := range batchCount {
		offset := batchRefreshOffset(batch, batchCount, spread)
		require.GreaterOrEqual(t, offset, time.Duration(batch)*slot)
		require.Less(t, offset, time.Duration(batch+1)*slot)
	}
	require.Zero(t, batchRefreshOffset(0, 1, spread))
}

func TestRefreshSessionLeasesUsesBoundedBatches(t *testing.T) {
	server := newTestServer()
	fakeStore := server.svcCtx.Store.(*fakeStore)
	presence := new(batchPresence)
	server.svcCtx.PresenceClient = presence
	for i := range 1001 {
		session := &logicalSession{
			id: "session-" + strconv.Itoa(i), userID: int64(i + 1), guilds: make(map[int64]struct{}),
		}
		server.sessions[session.id] = session
	}

	server.refreshSessionLeasesWithSpread(t.Context(), 0)

	require.Len(t, fakeStore.ownerBatches, 3)
	require.Len(t, fakeStore.ownerBatches[0], 500)
	require.Len(t, fakeStore.ownerBatches[1], 500)
	require.Len(t, fakeStore.ownerBatches[2], 1)
	require.Len(t, presence.requests, 3)
	require.Len(t, presence.requests[0].GetSessions(), 500)
	require.Len(t, presence.requests[1].GetSessions(), 500)
	require.Len(t, presence.requests[2].GetSessions(), 1)
}

func TestResumeExpandsBindingQueueForReplay(t *testing.T) {
	server := newTestServer()
	server.svcCtx.Cfg.Node.BindingQueueSize = 1
	identify := new(sessionv1.Identify)
	identify.SetToken("token")
	session, err := server.identify(t.Context(), "conn-a", "gateway-a", "gen-a", identify)
	require.NoError(t, err)

	session.mu.Lock()
	firstBinding := session.binding
	server.appendDispatchLocked(session, realtime.EventMessageCreated, []byte(`{"id":"1"}`))
	server.appendDispatchLocked(session, realtime.EventMessageUpdated, []byte(`{"id":"1"}`))
	session.mu.Unlock()
	server.detach(session, firstBinding, true)

	resume := new(sessionv1.Resume)
	resume.SetToken("token")
	resume.SetSessionId(session.id)
	resume.SetSequence(1)
	resumed, err := server.resume(t.Context(), "conn-b", "gateway-b", "gen-b", resume)
	require.NoError(t, err)

	resumed.mu.Lock()
	binding := resumed.binding
	resumed.mu.Unlock()
	require.Equal(t, 3, len(binding.send))
	require.Equal(t, 3, cap(binding.send))
}

func TestRegisterNodeUsesSessionRegistry(t *testing.T) {
	registry := &fakeRegistry{}
	server := newTestServerWithRegistry(registry)

	err := server.registerNode(t.Context(), sessionregistry.StatusReady)
	require.NoError(t, err)
	require.Equal(t, sessionregistry.Node{
		ID:         "session-test",
		Generation: server.generation,
		RPCAddress: "127.0.0.1:3006",
		Status:     sessionregistry.StatusReady,
	}, registry.node)
	require.Equal(t, 30*time.Second, registry.ttl)
}

func newTestServer() *Server {
	return newTestServerWithRegistry(&fakeRegistry{})
}

func newTestServerWithRegistry(registry *fakeRegistry) *Server {
	cfg := config.Config{
		Node: config.NodeConfig{
			ID: "session-test", AdvertiseAddress: "127.0.0.1:3006",
			SessionResumeSeconds: 120, MaxReplayEvents: 2048, BindingQueueSize: 4096,
		},
	}
	return New(svc.NewServiceContextWithDependencies(cfg, svc.Dependencies{
		Store:               &fakeStore{},
		SessionRegistry:     registry,
		AuthenticatorClient: fakeAuthenticator{},
		PresenceClient:      fakePresence{},
		GuildClient:         fakeGuild{},
	}))
}

type fakeStore struct {
	refreshed     []store.Route
	detached      []store.Route
	batchOwners   []store.Owner
	batchOwnerTTL time.Duration
	ownerBatches  [][]store.Owner
}

func (*fakeStore) SetOwner(context.Context, store.Owner, time.Duration) error { return nil }
func (s *fakeStore) SetOwners(_ context.Context, owners []store.Owner, ttl time.Duration) error {
	s.batchOwners = append([]store.Owner(nil), owners...)
	s.batchOwnerTTL = ttl
	s.ownerBatches = append(s.ownerBatches, append([]store.Owner(nil), owners...))
	return nil
}
func (*fakeStore) DeleteOwner(context.Context, string, string, string) error { return nil }
func (s *fakeStore) RefreshRoutes(_ context.Context, _, _ string, routes []store.Route, _ time.Duration) error {
	s.refreshed = append([]store.Route(nil), routes...)
	return nil
}
func (s *fakeStore) DetachRoutes(_ context.Context, _, _ string, routes []store.Route) error {
	s.detached = append(s.detached, routes...)
	return nil
}

type sessionRateCall struct {
	policy string
	key    string
	cost   int64
}

type sessionFakeRateLimiter struct {
	calls     []sessionRateCall
	decisions map[string]coreratelimit.Decision
}

func (l *sessionFakeRateLimiter) Take(_ context.Context, policy, key string, cost int64) (coreratelimit.Decision, error) {
	l.calls = append(l.calls, sessionRateCall{policy: policy, key: key, cost: cost})
	if decision, ok := l.decisions[policy]; ok {
		return decision, nil
	}
	return coreratelimit.Decision{Allowed: true}, nil
}

type fakeRegistry struct {
	node               sessionregistry.Node
	ttl                time.Duration
	resolveNode        sessionregistry.Node
	resolveErr         error
	resolvedNodeID     string
	resolvedGeneration string
}

func (r *fakeRegistry) Register(_ context.Context, node sessionregistry.Node, ttl time.Duration) error {
	r.node = node
	r.ttl = ttl
	return nil
}
func (*fakeRegistry) Ready(context.Context) ([]sessionregistry.Node, error) { return nil, nil }
func (r *fakeRegistry) Resolve(_ context.Context, nodeID, generation string) (sessionregistry.Node, error) {
	r.resolvedNodeID = nodeID
	r.resolvedGeneration = generation
	if r.resolveErr != nil {
		return sessionregistry.Node{}, r.resolveErr
	}
	if r.resolveNode.ID != "" {
		return r.resolveNode, nil
	}
	return sessionregistry.Node{}, sessionregistry.ErrNodeNotFound
}
func (*fakeRegistry) Close() error { return nil }

type fakeAuthenticator struct {
	authenticatorv1.AuthenticatorServiceClient
}

func (fakeAuthenticator) VerifyAccessToken(
	context.Context,
	*authenticatorv1.VerifyAccessTokenRequest,
	...grpc.CallOption,
) (*authenticatorv1.VerifyAccessTokenResponse, error) {
	resp := new(authenticatorv1.VerifyAccessTokenResponse)
	resp.SetOk(true)
	resp.SetUserId(1001)
	resp.SetSessionId(2002)
	resp.SetExpiresAt(3003)
	return resp, nil
}

type fakeGuild struct {
	guildv1.GuildServiceClient
}

func (fakeGuild) ListUserGuildChannelVisibilities(
	context.Context,
	*guildv1.ListUserGuildChannelVisibilitiesRequest,
	...grpc.CallOption,
) (*guildv1.ListUserGuildChannelVisibilitiesResponse, error) {
	return new(guildv1.ListUserGuildChannelVisibilitiesResponse), nil
}

func (fakeGuild) AuthorizeGuildChannel(
	context.Context,
	*guildv1.AuthorizeGuildChannelRequest,
	...grpc.CallOption,
) (*guildv1.AuthorizeGuildChannelResponse, error) {
	resp := new(guildv1.AuthorizeGuildChannelResponse)
	resp.SetAllowed(true)
	resp.SetGuildId(9001)
	return resp, nil
}

type fakePresence struct {
	presencev1.PresenceServiceClient
}

type recordingPresence struct {
	fakePresence
	updates []*presencev1.UpdateUserPresenceRequest
}

type batchPresence struct {
	fakePresence
	requests []*presencev1.RefreshUserSessionsRequest
}

func (p *batchPresence) RefreshUserSessions(
	_ context.Context,
	req *presencev1.RefreshUserSessionsRequest,
	_ ...grpc.CallOption,
) (*presencev1.RefreshUserSessionsResponse, error) {
	p.requests = append(p.requests, req)
	return new(presencev1.RefreshUserSessionsResponse), nil
}

func (p *recordingPresence) UpdateUserPresence(
	_ context.Context,
	req *presencev1.UpdateUserPresenceRequest,
	_ ...grpc.CallOption,
) (*presencev1.UpdateUserPresenceResponse, error) {
	p.updates = append(p.updates, req)
	return new(presencev1.UpdateUserPresenceResponse), nil
}

func (fakePresence) RegisterUserSession(
	context.Context,
	*presencev1.RegisterUserSessionRequest,
	...grpc.CallOption,
) (*presencev1.RegisterUserSessionResponse, error) {
	return new(presencev1.RegisterUserSessionResponse), nil
}

func (fakePresence) RefreshUserSession(
	context.Context,
	*presencev1.RefreshUserSessionRequest,
	...grpc.CallOption,
) (*presencev1.RefreshUserSessionResponse, error) {
	return new(presencev1.RefreshUserSessionResponse), nil
}

func (fakePresence) RefreshUserSessions(
	context.Context,
	*presencev1.RefreshUserSessionsRequest,
	...grpc.CallOption,
) (*presencev1.RefreshUserSessionsResponse, error) {
	return new(presencev1.RefreshUserSessionsResponse), nil
}

func (fakePresence) UpdateUserPresence(
	context.Context,
	*presencev1.UpdateUserPresenceRequest,
	...grpc.CallOption,
) (*presencev1.UpdateUserPresenceResponse, error) {
	return new(presencev1.UpdateUserPresenceResponse), nil
}

func (fakePresence) RemoveUserSession(
	context.Context,
	*presencev1.RemoveUserSessionRequest,
	...grpc.CallOption,
) (*presencev1.RemoveUserSessionResponse, error) {
	return new(presencev1.RemoveUserSessionResponse), nil
}
