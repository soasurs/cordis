package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
)

func TestIdentifyRetainsVisibilitySnapshotsAcrossLocalSessions(t *testing.T) {
	server := newTestServer()
	guild := &visibilityGuild{response: readyVisibilityResponse(
		readyVisibility(30, 7, 301, 302),
		readyVisibility(20, 4, 201),
	)}
	server.svcCtx.GuildClient = guild
	identify := new(sessionv1.Identify)
	identify.SetToken("token")

	first, err := server.identify(t.Context(), "conn-a", "gateway-a", "gen-a", identify)
	require.NoError(t, err)
	second, err := server.identify(t.Context(), "conn-b", "gateway-a", "gen-a", identify)
	require.NoError(t, err)
	require.Equal(t, 2, guild.calls)
	require.Equal(t, map[int64]struct{}{20: {}, 30: {}}, first.guilds)
	require.Equal(t, first.guilds, second.guilds)

	snapshot, ok := server.visibilitySnapshotFor(1001, 30)
	require.True(t, ok)
	require.Equal(t, int64(7), snapshot.accessRevision)
	require.True(t, snapshot.contains(301))
	require.False(t, snapshot.contains(999))
	server.visibilityMu.RLock()
	require.Equal(t, 2, server.visibilityUsers[1001].references)
	require.Same(t, snapshot, server.visibilityUsers[1001].snapshots[30].snapshot)
	server.visibilityMu.RUnlock()

	server.removeSession(t.Context(), first)
	server.removeSession(t.Context(), first)
	server.visibilityMu.RLock()
	require.Equal(t, 1, server.visibilityUsers[1001].references, "session removal is idempotent")
	server.visibilityMu.RUnlock()
	server.removeSession(t.Context(), second)
	server.visibilityMu.RLock()
	require.NotContains(t, server.visibilityUsers, int64(1001))
	server.visibilityMu.RUnlock()
}

func TestLoadReadyGuildsRejectsUnsafeResponses(t *testing.T) {
	t.Run("too many guilds", func(t *testing.T) {
		server := newTestServer()
		server.svcCtx.Cfg.Node.MaxVisibilityGuilds = 1
		server.svcCtx.GuildClient = &visibilityGuild{response: readyVisibilityResponse(
			readyVisibility(30, 1), readyVisibility(20, 1),
		)}
		_, _, err := server.loadReadyGuilds(t.Context(), 1001)
		require.Equal(t, codes.ResourceExhausted, status.Code(err))
	})

	t.Run("duplicate channels", func(t *testing.T) {
		server := newTestServer()
		server.svcCtx.GuildClient = &visibilityGuild{response: readyVisibilityResponse(
			readyVisibility(30, 1, 301, 301),
		)}
		_, _, err := server.loadReadyGuilds(t.Context(), 1001)
		require.Equal(t, codes.Internal, status.Code(err))
	})
}

func TestInvalidVisibilitySnapshotFailsClosed(t *testing.T) {
	server := newTestServer()
	server.retainVisibilitySnapshots(1001, map[int64]*visibilitySnapshot{
		30: {accessRevision: 7, channelIDs: []int64{301}},
	})
	_, ok := server.visibilitySnapshotFor(1001, 30)
	require.True(t, ok)
	require.False(t, server.invalidateVisibilityGuild(1001, 30, 6), "older events cannot invalidate a newer snapshot")
	_, ok = server.visibilitySnapshotFor(1001, 30)
	require.True(t, ok)

	require.True(t, server.invalidateVisibilityGuild(1001, 30, 8))
	_, ok = server.visibilitySnapshotFor(1001, 30)
	require.False(t, ok)
	_, ok = server.visibilitySnapshotFor(1001, 999)
	require.False(t, ok)

	server.retainVisibilitySnapshots(1002, map[int64]*visibilitySnapshot{
		30: {accessRevision: 7, channelIDs: []int64{301}},
	})
	require.True(t, server.invalidateVisibilityGuild(1002, 30, 0), "legacy events must invalidate conservatively")
	_, ok = server.visibilitySnapshotFor(1002, 30)
	require.False(t, ok)
}

func TestVisibilityReloadRejectsResultRacingWithNewerInvalidation(t *testing.T) {
	server := newTestServer()
	guild := &racingVisibilityGuild{started: make(chan struct{}), release: make(chan struct{})}
	server.svcCtx.GuildClient = guild
	session := testLogicalSession(1001, 9001)
	server.addSession(session, map[int64]*visibilitySnapshot{
		9001: {accessRevision: 7, channelIDs: []int64{7001}},
	})
	server.invalidateVisibilityGuild(1001, 9001, 8)

	type result struct {
		snapshot *visibilitySnapshot
		err      error
	}
	resultCh := make(chan result, 1)
	go func() {
		snapshot, err := server.ensureVisibilitySnapshot(t.Context(), 1001, 9001)
		resultCh <- result{snapshot: snapshot, err: err}
	}()
	<-guild.started
	server.invalidateVisibilityGuild(1001, 9001, 9)
	close(guild.release)
	loaded := <-resultCh

	require.NoError(t, loaded.err)
	require.Equal(t, int64(9), loaded.snapshot.accessRevision)
	require.Equal(t, 2, guild.calls)
}

type visibilityGuild struct {
	guildv1.GuildServiceClient
	response *guildv1.GetUserReadyStateResponse
	calls    int
}

type racingVisibilityGuild struct {
	guildv1.GuildServiceClient
	started chan struct{}
	release chan struct{}
	calls   int
}

func (g *visibilityGuild) GetUserReadyState(
	_ context.Context,
	_ *guildv1.GetUserReadyStateRequest,
	_ ...grpc.CallOption,
) (*guildv1.GetUserReadyStateResponse, error) {
	g.calls++
	return g.response, nil
}

func (g *visibilityGuild) GetUserGuildChannelVisibility(
	_ context.Context,
	req *guildv1.GetUserGuildChannelVisibilityRequest,
	_ ...grpc.CallOption,
) (*guildv1.GetUserGuildChannelVisibilityResponse, error) {
	for _, ready := range g.response.GetGuilds() {
		if ready.GetGuild().GetId() == req.GetGuildId() {
			resp := new(guildv1.GetUserGuildChannelVisibilityResponse)
			resp.SetVisibility(visibilityFromReady(ready))
			return resp, nil
		}
	}
	return nil, status.Error(codes.NotFound, "guild not found")
}

func (g *racingVisibilityGuild) GetUserGuildChannelVisibility(
	_ context.Context,
	_ *guildv1.GetUserGuildChannelVisibilityRequest,
	_ ...grpc.CallOption,
) (*guildv1.GetUserGuildChannelVisibilityResponse, error) {
	g.calls++
	revision := int64(9)
	if g.calls == 1 {
		close(g.started)
		<-g.release
		revision = 8
	}
	resp := new(guildv1.GetUserGuildChannelVisibilityResponse)
	resp.SetVisibility(visibility(9001, revision, 7001))
	return resp, nil
}

func readyVisibility(guildID, revision int64, channelIDs ...int64) *guildv1.ReadyGuild {
	guild := new(guildv1.Guild)
	guild.SetId(guildID)
	guild.SetOwnerId(1)
	guild.SetName("guild")
	guild.SetRevision(1)
	channels := make([]*guildv1.GuildChannel, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		channel := new(guildv1.GuildChannel)
		channel.SetId(channelID)
		channel.SetGuildId(guildID)
		channel.SetType(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT)
		channels = append(channels, channel)
	}
	ready := new(guildv1.ReadyGuild)
	ready.SetGuild(guild)
	ready.SetAccessRevision(revision)
	ready.SetChannels(channels)
	return ready
}

func readyVisibilityResponse(items ...*guildv1.ReadyGuild) *guildv1.GetUserReadyStateResponse {
	resp := new(guildv1.GetUserReadyStateResponse)
	resp.SetGuilds(items)
	return resp
}

func visibilityFromReady(ready *guildv1.ReadyGuild) *guildv1.GuildChannelVisibility {
	channelIDs := make([]int64, 0, len(ready.GetChannels()))
	for _, channel := range ready.GetChannels() {
		channelIDs = append(channelIDs, channel.GetId())
	}
	return visibility(ready.GetGuild().GetId(), ready.GetAccessRevision(), channelIDs...)
}

func visibility(guildID, revision int64, channelIDs ...int64) *guildv1.GuildChannelVisibility {
	item := new(guildv1.GuildChannelVisibility)
	item.SetGuildId(guildID)
	item.SetAccessRevision(revision)
	item.SetVisibleChannelIds(channelIDs)
	return item
}
