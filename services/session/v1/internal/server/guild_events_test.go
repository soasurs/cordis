package server

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/realtime"
)

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
