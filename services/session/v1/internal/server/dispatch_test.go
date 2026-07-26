package server

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/realtime"
)

func TestChannelOverwriteSkipsDeniedSession(t *testing.T) {
	guild := &authorizingGuild{allowed: false}
	server := newTestServerWithGuild(guild)
	session := testLogicalSession(1001, 9001)
	server.addSession(session, nil)

	req := guildEventRequest(9001, realtime.EventGuildChannelOverwriteUpdated, `{"guild_id":"9001","channel_id":"7001"}`)
	resp, err := server.DispatchGuildEvent(t.Context(), req)
	require.NoError(t, err)
	require.Zero(t, resp.GetDelivered())
}

func TestChannelOverwriteNotifiesPreviousViewerOnRevoke(t *testing.T) {
	guild := &authorizingGuild{allowed: false, accessRevision: 8}
	server := newTestServerWithGuild(guild)
	previousViewer := testLogicalSession(1001, 9001)
	neverViewer := testLogicalSession(1002, 9001)
	server.addSession(previousViewer, map[int64]*visibilitySnapshot{
		9001: {accessRevision: 7, channelIDs: []int64{7001}},
	})
	server.addSession(neverViewer, map[int64]*visibilitySnapshot{
		9001: {accessRevision: 7, channelIDs: []int64{7002}},
	})

	req := guildEventRequest(9001, realtime.EventGuildChannelOverwriteUpdated,
		`{"guild_id":"9001","channel_id":"7001","applies_to":1,"applies_to_id":"9001","access_revision":8}`)
	resp, err := server.DispatchGuildEvent(t.Context(), req)

	require.NoError(t, err)
	require.Equal(t, int32(1), resp.GetDelivered())
	require.Equal(t, realtime.EventGuildChannelOverwriteUpdated, previousViewer.replay[0].frame.GetType())
	require.Empty(t, neverViewer.replay)
	snapshot, ok := server.visibilitySnapshotFor(1001, 9001)
	require.True(t, ok)
	require.False(t, snapshot.contains(7001))
}

func TestChannelOverwriteNotifiesNewlyAuthorizedViewer(t *testing.T) {
	guild := &authorizingGuild{allowed: true, accessRevision: 8}
	server := newTestServerWithGuild(guild)
	session := testLogicalSession(1001, 9001)
	server.addSession(session, map[int64]*visibilitySnapshot{
		9001: {accessRevision: 7, channelIDs: []int64{7002}},
	})

	req := guildEventRequest(9001, realtime.EventGuildChannelOverwriteUpdated,
		`{"guild_id":"9001","channel_id":"7001","applies_to":1,"applies_to_id":"9001","access_revision":8}`)
	resp, err := server.DispatchGuildEvent(t.Context(), req)

	require.NoError(t, err)
	require.Equal(t, int32(1), resp.GetDelivered())
	require.Equal(t, realtime.EventGuildChannelOverwriteUpdated, session.replay[0].frame.GetType())
	snapshot, ok := server.visibilitySnapshotFor(1001, 9001)
	require.True(t, ok)
	require.True(t, snapshot.contains(7001))
}

func TestChannelOverwriteRevokeAfterGrantStillNotifies(t *testing.T) {
	guild := &authorizingGuild{allowed: true, accessRevision: 8}
	server := newTestServerWithGuild(guild)
	session := testLogicalSession(1001, 9001)
	server.addSession(session, map[int64]*visibilitySnapshot{
		9001: {accessRevision: 7, channelIDs: []int64{}},
	})

	grant := guildEventRequest(9001, realtime.EventGuildChannelOverwriteUpdated,
		`{"guild_id":"9001","channel_id":"7001","applies_to":1,"applies_to_id":"9001","access_revision":8}`)
	grant.GetEvent().SetIdempotencyKey(1)
	resp, err := server.DispatchGuildEvent(t.Context(), grant)
	require.NoError(t, err)
	require.Equal(t, int32(1), resp.GetDelivered())
	snapshot, ok := server.visibilitySnapshotFor(1001, 9001)
	require.True(t, ok)
	require.True(t, snapshot.contains(7001))

	guild.allowed = false
	guild.accessRevision = 9
	session.replay = session.replay[:0]
	revoke := guildEventRequest(9001, realtime.EventGuildChannelOverwriteUpdated,
		`{"guild_id":"9001","channel_id":"7001","applies_to":1,"applies_to_id":"9001","access_revision":9}`)
	revoke.GetEvent().SetIdempotencyKey(2)
	resp, err = server.DispatchGuildEvent(t.Context(), revoke)
	require.NoError(t, err)
	require.Equal(t, int32(1), resp.GetDelivered())
	require.Equal(t, realtime.EventGuildChannelOverwriteUpdated, session.replay[0].frame.GetType())
	snapshot, ok = server.visibilitySnapshotFor(1001, 9001)
	require.True(t, ok)
	require.False(t, snapshot.contains(7001))
}

func TestChannelAccessEventRefreshesDeniedViewerForLaterRevoke(t *testing.T) {
	guild := &authorizingGuild{
		accessRevision:    8,
		visibleChannelIDs: []int64{7002},
	}
	server := newTestServerWithGuild(guild)
	session := testLogicalSession(1001, 9001)
	server.addSession(session, map[int64]*visibilitySnapshot{
		9001: {accessRevision: 7, channelIDs: []int64{7002}},
	})

	unrelated := guildEventRequest(9001, realtime.EventGuildChannelOverwriteUpdated,
		`{"guild_id":"9001","channel_id":"7001","applies_to":1,"applies_to_id":"9001","access_revision":8}`)
	resp, err := server.DispatchGuildEvent(t.Context(), unrelated)
	require.NoError(t, err)
	require.Zero(t, resp.GetDelivered())
	require.Empty(t, session.replay)
	snapshot, ok := server.visibilitySnapshotFor(1001, 9001)
	require.True(t, ok)
	require.Equal(t, int64(8), snapshot.accessRevision)
	require.True(t, snapshot.contains(7002))

	guild.accessRevision = 9
	guild.visibleChannelIDs = []int64{}
	revoke := guildEventRequest(9001, realtime.EventGuildChannelOverwriteUpdated,
		`{"guild_id":"9001","channel_id":"7002","applies_to":1,"applies_to_id":"9001","access_revision":9}`)
	revoke.GetEvent().SetIdempotencyKey(2)
	resp, err = server.DispatchGuildEvent(t.Context(), revoke)

	require.NoError(t, err)
	require.Equal(t, int32(1), resp.GetDelivered())
	require.Equal(t, realtime.EventGuildChannelOverwriteUpdated, session.replay[0].frame.GetType())
	snapshot, ok = server.visibilitySnapshotFor(1001, 9001)
	require.True(t, ok)
	require.False(t, snapshot.contains(7002))
}

func TestChannelAccessReloadFailureRequestsReconciliation(t *testing.T) {
	server := newTestServerWithGuild(failingVisibilityGuild{})
	session := testLogicalSession(1001, 9001)
	server.addSession(session, map[int64]*visibilitySnapshot{
		9001: {accessRevision: 7, channelIDs: []int64{7001}},
	})

	resp, err := server.DispatchGuildEvent(
		t.Context(),
		guildEventRequest(9001, realtime.EventGuildChannelOverwriteUpdated,
			`{"guild_id":"9001","channel_id":"7001","applies_to":1,"applies_to_id":"9001","access_revision":8}`),
	)

	require.NoError(t, err)
	require.Zero(t, resp.GetDelivered())
	require.Len(t, session.replay, 1)
	require.Equal(t, realtime.GatewayEventReconcile, session.replay[0].frame.GetType())
	require.JSONEq(t, `{"guild_id":"9001","channel_id":"7001"}`, session.replay[0].frame.GetJsonPayload())
}

func TestChannelAccessReloadsUsersConcurrently(t *testing.T) {
	started := make(chan int64, 2)
	release := make(chan struct{})
	server := newTestServerWithGuild(&concurrentVisibilityGuild{
		started: started,
		release: release,
	})
	first := testLogicalSession(1001, 9001)
	second := testLogicalSession(1002, 9001)
	server.addSession(first, map[int64]*visibilitySnapshot{
		9001: {accessRevision: 7, channelIDs: []int64{7001}},
	})
	server.addSession(second, map[int64]*visibilitySnapshot{
		9001: {accessRevision: 7, channelIDs: []int64{7001}},
	})

	type dispatchResult struct {
		resp *sessionv1.DispatchGuildEventResponse
		err  error
	}
	result := make(chan dispatchResult, 1)
	go func() {
		resp, err := server.DispatchGuildEvent(
			t.Context(),
			guildEventRequest(9001, realtime.EventGuildChannelOverwriteUpdated,
				`{"guild_id":"9001","channel_id":"7001","applies_to":1,"applies_to_id":"9001","access_revision":8}`),
		)
		result <- dispatchResult{resp: resp, err: err}
	}()

	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("visibility reloads did not run concurrently")
		}
	}
	close(release)

	select {
	case got := <-result:
		require.NoError(t, got.err)
		require.Equal(t, int32(2), got.resp.GetDelivered())
	case <-time.After(time.Second):
		t.Fatal("channel access dispatch did not complete")
	}
}

func TestMemberOverwriteInvalidatesOnlyAffectedVisibilitySnapshot(t *testing.T) {
	guild := &authorizingGuild{allowed: true, accessRevision: 8}
	server := newTestServerWithGuild(guild)
	affected := testLogicalSession(1001, 9001)
	unaffected := testLogicalSession(1002, 9001)
	server.addSession(affected, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7001}}})
	server.addSession(unaffected, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7001}}})

	req := guildEventRequest(9001, realtime.EventGuildChannelOverwriteUpdated,
		`{"guild_id":"9001","channel_id":"7001","applies_to":2,"applies_to_id":"1001","access_revision":8}`)
	_, err := server.DispatchGuildEvent(t.Context(), req)

	require.NoError(t, err)
	snapshot, ok := server.visibilitySnapshotFor(1001, 9001)
	require.True(t, ok)
	require.Equal(t, int64(8), snapshot.accessRevision)
	snapshot, ok = server.visibilitySnapshotFor(1002, 9001)
	require.True(t, ok)
	require.Equal(t, int64(7), snapshot.accessRevision)
}

func TestMemberRoleUpdateInvalidatesAffectedUser(t *testing.T) {
	server := newTestServer()
	affected := testLogicalSession(1001, 9001)
	unaffected := testLogicalSession(1002, 9001)
	server.addSession(affected, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7001}}})
	server.addSession(unaffected, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7001}}})

	req := guildEventRequest(9001, realtime.EventGuildMemberRolesUpdated,
		`{"guild_id":"9001","user_id":"1001","access_revision":8}`)
	_, err := server.DispatchGuildEvent(t.Context(), req)
	require.NoError(t, err)
	_, ok := server.visibilitySnapshotFor(1001, 9001)
	require.False(t, ok)
	_, ok = server.visibilitySnapshotFor(1002, 9001)
	require.True(t, ok)
}

func TestMemberRemovalRevokesVisibilityAndGuildIndex(t *testing.T) {
	server := newTestServer()
	removed := testLogicalSession(1001, 9001)
	remaining := testLogicalSession(1002, 9001)
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

func TestChannelDeletedBroadcastsAndInvalidatesVisibility(t *testing.T) {
	server := newTestServer()
	first := testLogicalSession(1001, 9001)
	second := testLogicalSession(1002, 9001)
	server.addSession(first, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7001}}})
	server.addSession(second, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7001}}})

	req := guildEventRequest(9001, realtime.EventGuildChannelDeleted,
		`{"id":"7001","guild_id":"9001","access_revision":8}`)
	resp, err := server.DispatchGuildEvent(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int32(2), resp.GetDelivered())
	_, ok := server.visibilitySnapshotFor(1001, 9001)
	require.False(t, ok)
	_, ok = server.visibilitySnapshotFor(1002, 9001)
	require.False(t, ok)
}

func TestGuildMessageUsesVisibilitySnapshots(t *testing.T) {
	server := newTestServer()
	first := testLogicalSession(1001, 9001)
	second := testLogicalSession(1001, 9001)
	second.id = "session-1001-b"
	denied := testLogicalSession(1002, 9001)
	server.addSession(first, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7001}}})
	server.addSession(second, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7001}}})
	server.addSession(denied, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7002}}})

	req := channelEventRequest(9001, 7001, realtime.EventMessageCreated, `{"id":"8001"}`)
	resp, err := server.DispatchGuildMessageEvent(t.Context(), req)

	require.NoError(t, err)
	require.Equal(t, int32(2), resp.GetDelivered())
	require.Equal(t, realtime.EventMessageCreated, first.replay[0].frame.GetType())
	require.Equal(t, realtime.EventMessageCreated, second.replay[0].frame.GetType())
	require.Empty(t, denied.replay)
}

func TestGuildMessageRejectsMissingGuildID(t *testing.T) {
	server := newTestServer()

	_, err := server.DispatchGuildMessageEvent(
		t.Context(),
		channelEventRequest(0, 7001, realtime.EventMessageCreated, `{"id":"8001"}`),
	)

	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestDispatchRejectsMissingIdempotencyKey(t *testing.T) {
	server := newTestServer()

	guildReq := guildEventRequest(9001, realtime.EventGuildUpdated, `{"id":"9001"}`)
	guildReq.GetEvent().ClearIdempotencyKey()
	_, err := server.DispatchGuildEvent(t.Context(), guildReq)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	messageReq := channelEventRequest(9001, 7001, realtime.EventMessageCreated, `{"id":"8001"}`)
	messageReq.GetEvent().ClearIdempotencyKey()
	_, err = server.DispatchGuildMessageEvent(t.Context(), messageReq)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	event := new(sessionv1.EventEnvelope)
	event.SetType(realtime.EventRelationshipUpdated)
	event.SetJsonPayload(`{"user_id":"1001"}`)
	userReq := new(sessionv1.DispatchUserEventRequest)
	userReq.SetUserId(1001)
	userReq.SetEvent(event)
	_, err = server.DispatchUserEvent(t.Context(), userReq)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGuildMessageRejectsNonMessageEvent(t *testing.T) {
	server := newTestServer()
	req := channelEventRequest(9001, 7001, realtime.EventGuildUpdated, `{"id":"9001"}`)
	req.GetEvent().SetIdempotencyKey(100)

	_, firstErr := server.DispatchGuildMessageEvent(t.Context(), req)
	_, secondErr := server.DispatchGuildMessageEvent(t.Context(), req)

	require.Equal(t, codes.InvalidArgument, status.Code(firstErr))
	require.Equal(t, codes.InvalidArgument, status.Code(secondErr))
}

func TestGuildMessageDeduplicatesValidEvent(t *testing.T) {
	server := newTestServer()
	session := testLogicalSession(1001, 9001)
	server.addSession(session, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7001}}})
	req := channelEventRequest(9001, 7001, realtime.EventMessageCreated, `{"id":"8001"}`)
	req.GetEvent().SetIdempotencyKey(100)

	first, err := server.DispatchGuildMessageEvent(t.Context(), req)
	require.NoError(t, err)
	second, err := server.DispatchGuildMessageEvent(t.Context(), req)
	require.NoError(t, err)

	require.Equal(t, int32(1), first.GetDelivered())
	require.Zero(t, second.GetDelivered())
	require.Len(t, session.replay, 1)
}

func TestGuildMessageReloadsInvalidVisibilitySnapshot(t *testing.T) {
	server := newTestServer()
	server.svcCtx.GuildClient = &visibilityGuild{response: readyVisibilityResponse(
		readyVisibility(9001, 8, 7001),
	)}
	session := testLogicalSession(1001, 9001)
	server.addSession(session, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7002}}})
	require.True(t, server.invalidateVisibilityGuild(1001, 9001, 8))

	resp, err := server.DispatchGuildMessageEvent(
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
	session := testLogicalSession(1001, 9001)
	server.addSession(session, map[int64]*visibilitySnapshot{9001: {accessRevision: 7, channelIDs: []int64{7001}}})
	server.invalidateVisibilityGuild(1001, 9001, 8)
	req := channelEventRequest(9001, 7001, realtime.EventMessageCreated, `{"id":"8001"}`)

	first, err := server.DispatchGuildMessageEvent(t.Context(), req)
	require.NoError(t, err)
	second, err := server.DispatchGuildMessageEvent(t.Context(), req)
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

func testLogicalSession(userID, guildID int64) *logicalSession {
	return &logicalSession{
		id: "session-" + strconv.FormatInt(userID, 10), userID: userID,
		guilds: map[int64]struct{}{guildID: {}},
		replay: make([]replayEntry, 0),
	}
}

func guildEventRequest(guildID int64, eventType, payload string) *sessionv1.DispatchGuildEventRequest {
	event := new(sessionv1.EventEnvelope)
	event.SetType(eventType)
	event.SetJsonPayload(payload)
	event.SetIdempotencyKey(1)
	req := new(sessionv1.DispatchGuildEventRequest)
	req.SetGuildId(guildID)
	req.SetEvent(event)
	return req
}

func channelEventRequest(guildID, channelID int64, eventType, payload string) *sessionv1.DispatchGuildMessageEventRequest {
	event := new(sessionv1.EventEnvelope)
	event.SetType(eventType)
	event.SetJsonPayload(payload)
	event.SetIdempotencyKey(1)
	req := new(sessionv1.DispatchGuildMessageEventRequest)
	req.SetGuildId(guildID)
	req.SetChannelId(channelID)
	req.SetEvent(event)
	return req
}

type authorizingGuild struct {
	guildv1.GuildServiceClient
	allowed           bool
	accessRevision    int64
	visibleChannelIDs []int64
}

type failingVisibilityGuild struct {
	guildv1.GuildServiceClient
}

type concurrentVisibilityGuild struct {
	guildv1.GuildServiceClient
	started chan<- int64
	release <-chan struct{}
}

func (failingVisibilityGuild) GetUserReadyState(
	context.Context,
	*guildv1.GetUserReadyStateRequest,
	...grpc.CallOption,
) (*guildv1.GetUserReadyStateResponse, error) {
	return nil, status.Error(codes.Unavailable, "guild unavailable")
}

func (failingVisibilityGuild) GetUserGuildChannelVisibility(
	context.Context,
	*guildv1.GetUserGuildChannelVisibilityRequest,
	...grpc.CallOption,
) (*guildv1.GetUserGuildChannelVisibilityResponse, error) {
	return nil, status.Error(codes.Unavailable, "guild unavailable")
}

func (g *authorizingGuild) GetUserGuildChannelVisibility(
	_ context.Context,
	req *guildv1.GetUserGuildChannelVisibilityRequest,
	_ ...grpc.CallOption,
) (*guildv1.GetUserGuildChannelVisibilityResponse, error) {
	revision := g.accessRevision
	if revision <= 0 {
		revision = 8
	}
	channelIDs := append([]int64(nil), g.visibleChannelIDs...)
	if g.visibleChannelIDs == nil && g.allowed {
		channelIDs = []int64{7001}
	}
	resp := new(guildv1.GetUserGuildChannelVisibilityResponse)
	resp.SetVisibility(visibility(req.GetGuildId(), revision, channelIDs...))
	return resp, nil
}

func (g *concurrentVisibilityGuild) GetUserGuildChannelVisibility(
	ctx context.Context,
	req *guildv1.GetUserGuildChannelVisibilityRequest,
	_ ...grpc.CallOption,
) (*guildv1.GetUserGuildChannelVisibilityResponse, error) {
	select {
	case g.started <- req.GetUserId():
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-g.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	resp := new(guildv1.GetUserGuildChannelVisibilityResponse)
	resp.SetVisibility(visibility(req.GetGuildId(), 8, 7001))
	return resp, nil
}
