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

func TestIdentifySharesVisibilitySnapshotsAcrossLocalSessions(t *testing.T) {
	server := newTestServer()
	guild := &visibilityGuild{responses: []*guildv1.ListUserGuildChannelVisibilitiesResponse{
		visibilityResponse(visibility(30, 7, 301, 302), visibility(20, 4, 201)),
	}}
	server.svcCtx.GuildClient = guild
	identify := new(sessionv1.Identify)
	identify.SetToken("token")

	first, err := server.identify(t.Context(), "conn-a", "gateway-a", "gen-a", identify)
	require.NoError(t, err)
	second, err := server.identify(t.Context(), "conn-b", "gateway-a", "gen-a", identify)
	require.NoError(t, err)
	require.Equal(t, 1, guild.calls, "the second local session reuses the user snapshot")
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

func TestLoadVisibilitySnapshotsPaginatesWithinBounds(t *testing.T) {
	server := newTestServer()
	server.svcCtx.Cfg.Node.MaxVisibilityGuilds = 101
	firstPage := make([]*guildv1.GuildChannelVisibility, 0, guildPageSize)
	for guildID := int64(200); guildID >= 101; guildID-- {
		firstPage = append(firstPage, visibility(guildID, guildID, guildID*10))
	}
	first := visibilityResponse(firstPage...)
	first.SetBeforeGuildId(101)
	server.svcCtx.GuildClient = &visibilityGuild{responses: []*guildv1.ListUserGuildChannelVisibilitiesResponse{
		first,
		visibilityResponse(visibility(100, 100, 1000)),
	}}

	snapshots, err := server.loadVisibilitySnapshots(t.Context(), 1001)
	require.NoError(t, err)
	require.Len(t, snapshots, 101)
	require.Equal(t, []int64{0, 101}, server.svcCtx.GuildClient.(*visibilityGuild).before)
}

func TestLoadVisibilitySnapshotsRejectsUnsafeResponses(t *testing.T) {
	t.Run("too many guilds", func(t *testing.T) {
		server := newTestServer()
		server.svcCtx.Cfg.Node.MaxVisibilityGuilds = 1
		server.svcCtx.GuildClient = &visibilityGuild{responses: []*guildv1.ListUserGuildChannelVisibilitiesResponse{
			visibilityResponse(visibility(30, 1), visibility(20, 1)),
		}}
		_, err := server.loadVisibilitySnapshots(t.Context(), 1001)
		require.Equal(t, codes.ResourceExhausted, status.Code(err))
	})

	t.Run("unsorted channels", func(t *testing.T) {
		server := newTestServer()
		server.svcCtx.GuildClient = &visibilityGuild{responses: []*guildv1.ListUserGuildChannelVisibilitiesResponse{
			visibilityResponse(visibility(30, 1, 302, 301)),
		}}
		_, err := server.loadVisibilitySnapshots(t.Context(), 1001)
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
	session := testLogicalSession(1001, 9001, 0)
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
	responses []*guildv1.ListUserGuildChannelVisibilitiesResponse
	calls     int
	before    []int64
}

type racingVisibilityGuild struct {
	guildv1.GuildServiceClient
	started chan struct{}
	release chan struct{}
	calls   int
}

func (g *racingVisibilityGuild) ListUserGuildChannelVisibilities(
	_ context.Context,
	_ *guildv1.ListUserGuildChannelVisibilitiesRequest,
	_ ...grpc.CallOption,
) (*guildv1.ListUserGuildChannelVisibilitiesResponse, error) {
	g.calls++
	if g.calls == 1 {
		close(g.started)
		<-g.release
		return visibilityResponse(visibility(9001, 8, 7001)), nil
	}
	return visibilityResponse(visibility(9001, 9, 7001)), nil
}

func (g *visibilityGuild) ListUserGuildChannelVisibilities(
	_ context.Context,
	req *guildv1.ListUserGuildChannelVisibilitiesRequest,
	_ ...grpc.CallOption,
) (*guildv1.ListUserGuildChannelVisibilitiesResponse, error) {
	g.before = append(g.before, req.GetBeforeGuildId())
	if g.calls >= len(g.responses) {
		return new(guildv1.ListUserGuildChannelVisibilitiesResponse), nil
	}
	resp := g.responses[g.calls]
	g.calls++
	return resp, nil
}

func visibility(guildID, revision int64, channelIDs ...int64) *guildv1.GuildChannelVisibility {
	item := new(guildv1.GuildChannelVisibility)
	item.SetGuildId(guildID)
	item.SetAccessRevision(revision)
	item.SetVisibleChannelIds(channelIDs)
	return item
}

func visibilityResponse(items ...*guildv1.GuildChannelVisibility) *guildv1.ListUserGuildChannelVisibilitiesResponse {
	resp := new(guildv1.ListUserGuildChannelVisibilitiesResponse)
	resp.SetVisibilities(items)
	if len(items) > 0 {
		resp.SetBeforeGuildId(items[len(items)-1].GetGuildId())
	}
	return resp
}
