package server

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

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

type authorizingGuild struct {
	guildv1.GuildServiceClient
	allowed bool
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
