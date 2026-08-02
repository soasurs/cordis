package server

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/soasurs/cordis/pkg/realtime"
)

func TestProfileUpdateDeduplicatesAcrossGuildAndUserRoutes(t *testing.T) {
	server := newTestServer()
	firstClient := testLogicalSession(1002, 9001)
	firstClient.guilds[9002] = struct{}{}
	secondClient := testLogicalSession(1002, 9001)
	secondClient.id += "-second"
	secondClient.guilds[9002] = struct{}{}
	otherMember := testLogicalSession(1003, 9001)
	dmRecipient := testLogicalSession(1004, 0)
	server.addSession(firstClient, nil)
	server.addSession(secondClient, nil)
	server.addSession(otherMember, nil)
	server.addSession(dmRecipient, nil)

	payload := `{"user_id":"1001","name":"Updated"}`
	firstGuild := guildEventRequest(9001, realtime.EventUserProfileUpdated, payload)
	firstGuild.GetEvent().SetIdempotencyKey(101)
	resp, err := server.DispatchGuildEvent(t.Context(), firstGuild)
	require.NoError(t, err)
	require.Equal(t, int32(3), resp.GetDelivered())

	secondGuild := guildEventRequest(9002, realtime.EventUserProfileUpdated, payload)
	secondGuild.GetEvent().SetIdempotencyKey(101)
	resp, err = server.DispatchGuildEvent(t.Context(), secondGuild)
	require.NoError(t, err)
	require.Zero(t, resp.GetDelivered())

	directDuplicate := userEventRequest(1002, realtime.EventUserProfileUpdated, payload)
	directDuplicate.GetEvent().SetIdempotencyKey(101)
	userResp, err := server.DispatchUserEvent(t.Context(), directDuplicate)
	require.NoError(t, err)
	require.Zero(t, userResp.GetDelivered())

	direct := userEventRequest(1004, realtime.EventUserProfileUpdated, payload)
	direct.GetEvent().SetIdempotencyKey(101)
	userResp, err = server.DispatchUserEvent(t.Context(), direct)
	require.NoError(t, err)
	require.Equal(t, int32(1), userResp.GetDelivered())

	for _, session := range []*logicalSession{firstClient, secondClient, otherMember, dmRecipient} {
		require.Len(t, session.replay, 1)
		require.Equal(t, realtime.EventUserProfileUpdated, session.replay[0].frame.GetType())
	}
}

func TestPresenceUpdateDeduplicatesAcrossGuildAndUserRoutes(t *testing.T) {
	server := newTestServer()
	session := testLogicalSession(1001, 9001)
	session.guilds[9002] = struct{}{}
	server.addSession(session, nil)
	payload := `{"user_id":"1001","status":3,"version":"101"}`

	first := guildEventRequest(9001, realtime.EventPresenceUpdated, payload)
	first.GetEvent().SetIdempotencyKey(101)
	firstResp, err := server.DispatchGuildEvent(t.Context(), first)
	require.NoError(t, err)
	require.Equal(t, int32(1), firstResp.GetDelivered())

	second := guildEventRequest(9002, realtime.EventPresenceUpdated, payload)
	second.GetEvent().SetIdempotencyKey(101)
	secondResp, err := server.DispatchGuildEvent(t.Context(), second)
	require.NoError(t, err)
	require.Zero(t, secondResp.GetDelivered())

	direct := userEventRequest(1001, realtime.EventPresenceUpdated, payload)
	direct.GetEvent().SetIdempotencyKey(101)
	directResp, err := server.DispatchUserEvent(t.Context(), direct)
	require.NoError(t, err)
	require.Zero(t, directResp.GetDelivered())
	require.Len(t, session.replay, 1)
}

func TestPresencePreferenceRejectsGuildRoute(t *testing.T) {
	server := newTestServer()
	req := guildEventRequest(
		9001,
		realtime.EventPresencePreferenceUpdated,
		`{"user_id":"1001","status":"invisible","version":"101"}`,
	)

	_, err := server.DispatchGuildEvent(t.Context(), req)

	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestProfileUpdateRejectsMissingSubject(t *testing.T) {
	server := newTestServer()

	_, err := server.DispatchGuildEvent(
		t.Context(),
		guildEventRequest(9001, realtime.EventUserProfileUpdated, `{"name":"Updated"}`),
	)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = server.DispatchUserEvent(
		t.Context(),
		userEventRequest(1001, realtime.EventUserProfileUpdated, `{"name":"Updated"}`),
	)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
