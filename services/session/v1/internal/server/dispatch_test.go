package server

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/session/v1/internal/store"
)

func TestChannelOverwriteRevokesDeniedSubscription(t *testing.T) {
	guild := &authorizingGuild{allowed: false}
	server := newTestServerWithGuild(guild)
	session := testLogicalSession(1001, 9001, 7001)
	server.addSession(session, nil)
	server.addChannelIndexes(session, []int64{7001})

	req := guildEventRequest(9001, realtime.EventGuildChannelOverwriteUpdated, `{"guild_id":"9001","channel_id":"7001"}`)
	resp, err := server.DispatchGuildEvent(t.Context(), req)
	require.NoError(t, err)
	require.Zero(t, resp.GetDelivered())
	require.Empty(t, server.channelSessions(7001))
	require.NotContains(t, session.channels, int64(7001))
}

func TestMemberOverwriteInvalidatesOnlyAffectedVisibilitySnapshot(t *testing.T) {
	guild := &authorizingGuild{allowed: true}
	server := newTestServerWithGuild(guild)
	affected := testLogicalSession(1001, 9001, 0)
	unaffected := testLogicalSession(1002, 9001, 0)
	server.addSession(affected, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7001}}})
	server.addSession(unaffected, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7001}}})

	req := guildEventRequest(9001, realtime.EventGuildChannelOverwriteUpdated,
		`{"guild_id":"9001","channel_id":"7001","target_type":2,"target_id":"1001","access_revision":8}`)
	_, err := server.DispatchGuildEvent(t.Context(), req)

	require.NoError(t, err)
	_, ok := server.visibilitySnapshotFor(1001, 9001)
	require.False(t, ok)
	snapshot, ok := server.visibilitySnapshotFor(1002, 9001)
	require.True(t, ok)
	require.Equal(t, int64(7), snapshot.accessRevision)
}

func TestMemberRoleUpdateReauthorizesAffectedUser(t *testing.T) {
	guild := &authorizingGuild{allowed: false}
	server := newTestServerWithGuild(guild)
	affected := testLogicalSession(1001, 9001, 7001)
	unaffected := testLogicalSession(1002, 9001, 7001)
	server.addSession(affected, nil)
	server.addSession(unaffected, nil)
	server.addChannelIndexes(affected, []int64{7001})
	server.addChannelIndexes(unaffected, []int64{7001})

	req := guildEventRequest(9001, realtime.EventGuildMemberRolesUpdated, `{"guild_id":"9001","user_id":"1001"}`)
	_, err := server.DispatchGuildEvent(t.Context(), req)
	require.NoError(t, err)
	require.NotContains(t, affected.channels, int64(7001))
	require.Contains(t, unaffected.channels, int64(7001))
}

func TestMemberRemovalRevokesVisibilityAndGuildIndex(t *testing.T) {
	server := newTestServer()
	removed := testLogicalSession(1001, 9001, 0)
	remaining := testLogicalSession(1002, 9001, 0)
	server.addSession(removed, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7001}}})
	server.addSession(remaining, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7001}}})

	resp, err := server.DispatchGuildEvent(
		t.Context(),
		guildEventRequest(9001, realtime.EventGuildMemberRemoved,
			`{"guild_id":"9001","user_id":"1001","access_revision":8}`),
	)

	require.NoError(t, err)
	require.Equal(t, int32(2), resp.GetDelivered())
	require.NotContains(t, removed.guilds, int64(9001))
	require.NotContains(t, server.guildSessions(9001), removed)
	_, ok := server.visibilitySnapshotFor(1001, 9001)
	require.False(t, ok)
	_, ok = server.visibilitySnapshotFor(1002, 9001)
	require.True(t, ok)
}

func TestChannelDeletedBroadcastsAndCleansSubscriptions(t *testing.T) {
	server := newTestServer()
	subscribed := testLogicalSession(1001, 9001, 7001)
	guildOnly := testLogicalSession(1002, 9001, 0)
	server.addSession(subscribed, nil)
	server.addSession(guildOnly, nil)
	server.addChannelIndexes(subscribed, []int64{7001})

	req := guildEventRequest(9001, realtime.EventGuildChannelDeleted, `{"id":"7001","guild_id":"9001"}`)
	resp, err := server.DispatchGuildEvent(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int32(2), resp.GetDelivered())
	require.Empty(t, server.channelSessions(7001))
	require.NotContains(t, subscribed.channels, int64(7001))
	require.NotContains(t, subscribed.channelGuilds, int64(7001))
}

func TestChannelRemovalDetachesPublishedRoute(t *testing.T) {
	server := newTestServer()
	session := testLogicalSession(1001, 9001, 7001)
	server.addSession(session, nil)
	server.addChannelIndexes(session, []int64{7001})
	server.refreshAllRoutes(t.Context())

	server.unsubscribeSessionChannel(session, 7001)

	fakeStore := server.svcCtx.Store.(*fakeStore)
	require.Contains(t, fakeStore.detached, store.Route{Kind: store.RouteChannel, ID: 7001})
}

func TestGuildMessageUsesVisibilitySnapshotsInsteadOfSubscriptions(t *testing.T) {
	server := newTestServer()
	first := testLogicalSession(1001, 9001, 0)
	second := testLogicalSession(1001, 9001, 0)
	second.id = "session-1001-b"
	denied := testLogicalSession(1002, 9001, 7001)
	server.addSession(first, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7001}}})
	server.addSession(second, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7001}}})
	server.addSession(denied, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7002}}})

	req := channelEventRequest(9001, 7001, realtime.EventMessageCreated, `{"id":"8001"}`)
	resp, err := server.DispatchChannelEvent(t.Context(), req)

	require.NoError(t, err)
	require.Equal(t, int32(2), resp.GetDelivered())
	require.Equal(t, realtime.EventMessageCreated, first.replay[0].frame.GetType())
	require.Equal(t, realtime.EventMessageCreated, second.replay[0].frame.GetType())
	require.Empty(t, denied.replay, "a legacy channel subscription cannot bypass visibility")
}

func TestLegacyChannelEventUsesSubscriptions(t *testing.T) {
	server := newTestServer()
	subscribed := testLogicalSession(1001, 9001, 7001)
	guildOnly := testLogicalSession(1002, 9001, 0)
	server.addSession(subscribed, nil)
	server.addSession(guildOnly, nil)
	server.addChannelIndexes(subscribed, []int64{7001})

	resp, err := server.DispatchChannelEvent(
		t.Context(),
		channelEventRequest(0, 7001, realtime.EventMessageCreated, `{"id":"8001"}`),
	)

	require.NoError(t, err)
	require.Equal(t, int32(1), resp.GetDelivered())
	require.Len(t, subscribed.replay, 1)
	require.Empty(t, guildOnly.replay)
}

func TestChannelEventRejectsNegativeGuildID(t *testing.T) {
	server := newTestServer()

	_, err := server.DispatchChannelEvent(
		t.Context(),
		channelEventRequest(-1, 7001, realtime.EventMessageCreated, `{"id":"8001"}`),
	)

	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGuildMessageReloadsInvalidVisibilitySnapshot(t *testing.T) {
	server := newTestServer()
	server.svcCtx.GuildClient = &visibilityGuild{responses: []*guildv1.ListUserGuildChannelVisibilitiesResponse{
		visibilityResponse(visibility(9001, 8, 7001)),
	}}
	session := testLogicalSession(1001, 9001, 0)
	server.addSession(session, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7002}}})
	require.True(t, server.invalidateVisibilityGuild(1001, 9001, 8))

	resp, err := server.DispatchChannelEvent(
		t.Context(),
		channelEventRequest(9001, 7001, realtime.EventMessageCreated, `{"id":"8001"}`),
	)

	require.NoError(t, err)
	require.Equal(t, int32(1), resp.GetDelivered())
	snapshot, ok := server.visibilitySnapshotFor(1001, 9001)
	require.True(t, ok)
	require.Equal(t, int64(8), snapshot.accessRevision)
}

func TestGuildMessageReloadFailureRequestsReconciliationOnce(t *testing.T) {
	server := newTestServer()
	server.svcCtx.GuildClient = failingVisibilityGuild{}
	session := testLogicalSession(1001, 9001, 0)
	server.addSession(session, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7001}}})
	server.invalidateVisibilityGuild(1001, 9001, 8)
	req := channelEventRequest(9001, 7001, realtime.EventMessageCreated, `{"id":"8001"}`)

	first, err := server.DispatchChannelEvent(t.Context(), req)
	require.NoError(t, err)
	second, err := server.DispatchChannelEvent(t.Context(), req)
	require.NoError(t, err)

	require.Zero(t, first.GetDelivered())
	require.Zero(t, second.GetDelivered())
	require.Len(t, session.replay, 1)
	require.Equal(t, realtime.GatewayEventReconcile, session.replay[0].frame.GetType())
	require.JSONEq(t, `{"guild_id":"9001","channel_id":"7001"}`, session.replay[0].frame.GetJsonPayload())
}

func newTestServerWithGuild(guild guildv1.GuildServiceClient) *Server {
	server := newTestServer()
	server.svcCtx.GuildClient = guild
	return server
}

func testLogicalSession(userID, guildID, channelID int64) *logicalSession {
	session := &logicalSession{
		id: "session-" + strconv.FormatInt(userID, 10), userID: userID,
		guilds:   map[int64]struct{}{guildID: {}},
		channels: make(map[int64]struct{}), channelGuilds: make(map[int64]int64),
		replay: make([]replayEntry, 0),
	}
	if channelID > 0 {
		session.channels[channelID] = struct{}{}
		session.channelGuilds[channelID] = guildID
	}
	return session
}

func guildEventRequest(guildID int64, eventType, payload string) *sessionv1.DispatchGuildEventRequest {
	event := new(sessionv1.EventEnvelope)
	event.SetType(eventType)
	event.SetJsonPayload(payload)
	req := new(sessionv1.DispatchGuildEventRequest)
	req.SetGuildId(guildID)
	req.SetEvent(event)
	return req
}

func channelEventRequest(guildID, channelID int64, eventType, payload string) *sessionv1.DispatchChannelEventRequest {
	event := new(sessionv1.EventEnvelope)
	event.SetType(eventType)
	event.SetJsonPayload(payload)
	req := new(sessionv1.DispatchChannelEventRequest)
	req.SetGuildId(guildID)
	req.SetChannelId(channelID)
	req.SetEvent(event)
	return req
}

type authorizingGuild struct {
	guildv1.GuildServiceClient
	allowed bool
}

type failingVisibilityGuild struct {
	guildv1.GuildServiceClient
}

func (failingVisibilityGuild) ListUserGuildChannelVisibilities(
	context.Context,
	*guildv1.ListUserGuildChannelVisibilitiesRequest,
	...grpc.CallOption,
) (*guildv1.ListUserGuildChannelVisibilitiesResponse, error) {
	return nil, status.Error(codes.Unavailable, "guild unavailable")
}

func (g *authorizingGuild) AuthorizeGuildChannel(
	context.Context,
	*guildv1.AuthorizeGuildChannelRequest,
	...grpc.CallOption,
) (*guildv1.AuthorizeGuildChannelResponse, error) {
	resp := new(guildv1.AuthorizeGuildChannelResponse)
	resp.SetAllowed(g.allowed)
	resp.SetGuildId(9001)
	return resp, nil
}
