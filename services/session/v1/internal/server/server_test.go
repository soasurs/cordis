package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/pkg/sessionregistry"
	"github.com/soasurs/cordis/services/session/v1/config"
	"github.com/soasurs/cordis/services/session/v1/internal/store"
	"github.com/soasurs/cordis/services/session/v1/internal/svc"
)

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

	const channelID = int64(9007199254740993)
	session.mu.Lock()
	binding := session.binding
	session.mu.Unlock()
	require.NoError(t, server.subscribeChannels(t.Context(), session, binding, []int64{channelID}))

	var subscribed map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(session.replay[len(session.replay)-1].frame.GetJsonPayload()), &subscribed))
	require.JSONEq(t, `["9007199254740993"]`, string(subscribed["channel_ids"]))
}

func TestSubscribeEnforcesTotalChannelLimitAtomically(t *testing.T) {
	server := newTestServer()
	server.svcCtx.Cfg.Node.MaxSubscribedChannels = 2
	identify := new(sessionv1.Identify)
	identify.SetToken("token")
	session, err := server.identify(t.Context(), "conn-limit", "gateway-a", "gen-a", identify)
	require.NoError(t, err)

	session.mu.Lock()
	binding := session.binding
	session.mu.Unlock()
	require.NoError(t, server.subscribeChannels(t.Context(), session, binding, []int64{10, 11}))
	// Re-subscribing an existing channel consumes no additional capacity.
	require.NoError(t, server.subscribeChannels(t.Context(), session, binding, []int64{11}))

	err = server.subscribeChannels(t.Context(), session, binding, []int64{12})
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	session.mu.Lock()
	require.Len(t, session.channels, 2)
	require.NotContains(t, session.channels, int64(12))
	session.mu.Unlock()
}

func TestReplayWindowKeepsLatestEvents(t *testing.T) {
	server := newTestServer()
	server.svcCtx.Cfg.Node.MaxReplayEvents = 3
	session := &logicalSession{
		guilds: make(map[int64]struct{}), channels: make(map[int64]struct{}),
		channelGuilds: make(map[int64]int64),
	}
	for i := 0; i < 5; i++ {
		server.appendDispatchLocked(session, realtime.EventMessageCreated, []byte(`{}`))
	}
	require.Equal(t, uint64(5), session.sequence)
	require.Equal(t, uint64(2), session.replayFloor)
	require.Len(t, session.replay, 3)
	require.Equal(t, uint64(3), session.replay[0].sequence)
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
		MessageClient:       fakeMessage{},
	}))
}

type fakeStore struct {
	refreshed []store.Route
	detached  []store.Route
}

func (*fakeStore) SetOwner(context.Context, store.Owner, time.Duration) error { return nil }
func (*fakeStore) DeleteOwner(context.Context, string, string, string) error  { return nil }
func (s *fakeStore) RefreshRoutes(_ context.Context, _, _ string, routes []store.Route, _ time.Duration) error {
	s.refreshed = append([]store.Route(nil), routes...)
	return nil
}
func (s *fakeStore) DetachRoutes(_ context.Context, _, _ string, routes []store.Route) error {
	s.detached = append(s.detached, routes...)
	return nil
}

type fakeRegistry struct {
	node sessionregistry.Node
	ttl  time.Duration
}

func (r *fakeRegistry) Register(_ context.Context, node sessionregistry.Node, ttl time.Duration) error {
	r.node = node
	r.ttl = ttl
	return nil
}
func (*fakeRegistry) Ready(context.Context) ([]sessionregistry.Node, error) { return nil, nil }
func (*fakeRegistry) Resolve(context.Context, string, string) (sessionregistry.Node, error) {
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

type fakeMessage struct {
	messagev1.MessageServiceClient
}

func (fakeMessage) AuthorizeDmChannel(context.Context, *messagev1.AuthorizeDmChannelRequest, ...grpc.CallOption) (*messagev1.AuthorizeDmChannelResponse, error) {
	return nil, status.Error(codes.NotFound, "dm channel not found")
}

type fakeGuild struct {
	guildv1.GuildServiceClient
}

func (fakeGuild) ListUserGuilds(
	context.Context,
	*guildv1.ListUserGuildsRequest,
	...grpc.CallOption,
) (*guildv1.ListUserGuildsResponse, error) {
	return new(guildv1.ListUserGuildsResponse), nil
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

// notFoundGuild simulates Guild's response for channels it does not own.
type notFoundGuild struct {
	guildv1.GuildServiceClient
}

func (notFoundGuild) ListUserGuilds(context.Context, *guildv1.ListUserGuildsRequest, ...grpc.CallOption) (*guildv1.ListUserGuildsResponse, error) {
	return new(guildv1.ListUserGuildsResponse), nil
}

func (notFoundGuild) AuthorizeGuildChannel(context.Context, *guildv1.AuthorizeGuildChannelRequest, ...grpc.CallOption) (*guildv1.AuthorizeGuildChannelResponse, error) {
	return nil, status.Error(codes.NotFound, "guild channel not found")
}

type dmMessage struct {
	messagev1.MessageServiceClient
	allowed bool
}

func (m dmMessage) AuthorizeDmChannel(context.Context, *messagev1.AuthorizeDmChannelRequest, ...grpc.CallOption) (*messagev1.AuthorizeDmChannelResponse, error) {
	resp := new(messagev1.AuthorizeDmChannelResponse)
	resp.SetAllowed(m.allowed)
	return resp, nil
}

func TestSubscribeFallsBackToDmChannels(t *testing.T) {
	server := newTestServer()
	server.svcCtx.GuildClient = notFoundGuild{}
	server.svcCtx.MessageClient = dmMessage{allowed: true}

	identify := new(sessionv1.Identify)
	identify.SetToken("token")
	session, err := server.identify(t.Context(), "conn-dm", "gateway-a", "gen-a", identify)
	require.NoError(t, err)

	session.mu.Lock()
	binding := session.binding
	session.mu.Unlock()
	require.NoError(t, server.subscribeChannels(t.Context(), session, binding, []int64{555}))
	session.mu.Lock()
	_, subscribed := session.channels[555]
	require.Zero(t, session.channelGuilds[555])
	session.mu.Unlock()
	require.True(t, subscribed)

	// A non-participant is rejected.
	server.svcCtx.MessageClient = dmMessage{allowed: false}
	other, err := server.identify(t.Context(), "conn-dm-2", "gateway-a", "gen-a", identify)
	require.NoError(t, err)
	other.mu.Lock()
	otherBinding := other.binding
	other.mu.Unlock()
	err = server.subscribeChannels(t.Context(), other, otherBinding, []int64{555})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}
