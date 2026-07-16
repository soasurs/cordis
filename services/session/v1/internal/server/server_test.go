package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/realtime"
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

func newTestServer() *Server {
	cfg := config.Config{
		Node: config.NodeConfig{
			ID: "session-test", AdvertiseAddress: "127.0.0.1:3006",
			SessionResumeSeconds: 120, MaxReplayEvents: 2048, BindingQueueSize: 4096,
		},
	}
	return New(svc.NewServiceContextWithDependencies(cfg, svc.Dependencies{
		Store:               &fakeStore{},
		AuthenticatorClient: fakeAuthenticator{},
		PresenceClient:      fakePresence{},
		GuildClient:         fakeGuild{},
	}))
}

type fakeStore struct{}

func (*fakeStore) RegisterNode(context.Context, store.Node, time.Duration) error { return nil }
func (*fakeStore) SetOwner(context.Context, store.Owner, time.Duration) error    { return nil }
func (*fakeStore) DeleteOwner(context.Context, string, string, string) error     { return nil }
func (*fakeStore) RefreshRoutes(context.Context, string, string, []store.Route, time.Duration) error {
	return nil
}
func (*fakeStore) DetachRoutes(context.Context, string, string, []store.Route) error { return nil }

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
