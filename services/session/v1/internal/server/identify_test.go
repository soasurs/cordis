package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/realtime"
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

func TestIdentifyAcceptsGatewayTicket(t *testing.T) {
	server := newTestServer()
	identify := new(sessionv1.Identify)
	identify.SetGatewayTicket("ticket")
	session, err := server.identify(t.Context(), "conn-ticket", "gateway", "generation", identify)
	require.NoError(t, err)
	require.Equal(t, int64(1001), session.userID)
	require.Equal(t, int64(2002), session.authSessionID)
}

func TestIdentifyRejectsUnsuccessfulGatewayTicket(t *testing.T) {
	server := newTestServer()
	server.svcCtx.AuthenticatorClient = rejectedGatewayTicketAuthenticator{}
	identify := new(sessionv1.Identify)
	identify.SetGatewayTicket("ticket")

	_, err := server.identify(t.Context(), "conn-ticket", "gateway", "generation", identify)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
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
	require.JSONEq(t, `[]`, string(ready["guilds"]))
	require.JSONEq(t, `[]`, string(ready["dm_channels"]))
	require.JSONEq(t, `[]`, string(ready["read_states"]))

}

func TestIdentifyReadyContainsGuildsDMsAndReadStates(t *testing.T) {
	server := newTestServer()
	readyGuild := readyVisibility(9001, 7, 7001)
	readyGuild.SetChannelLayoutRevision(11)
	readyGuild.GetGuild().SetOwnerId(1001)
	readyGuild.GetGuild().SetDescription("Community description")
	role := new(guildv1.GuildRole)
	role.SetId(9001)
	role.SetGuildId(9001)
	role.SetPermissions(42)
	readyGuild.SetRoles([]*guildv1.GuildRole{role})
	readyGuild.SetMemberRoleIds([]int64{9002})
	overwrite := new(guildv1.GuildChannelPermissionOverwrite)
	overwrite.SetChannelId(7001)
	overwrite.SetGuildId(9001)
	overwrite.SetAppliesTo(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE)
	overwrite.SetAppliesToId(9001)
	overwrite.SetAllow(1024)
	readyGuild.SetPermissionOverwrites([]*guildv1.GuildChannelPermissionOverwrite{overwrite})
	server.svcCtx.GuildClient = &visibilityGuild{response: readyVisibilityResponse(readyGuild)}

	dm := new(messagev1.DmChannel)
	dm.SetId(8001)
	dm.SetUserLo(1001)
	dm.SetUserHi(1002)
	state := new(messagev1.ChannelReadState)
	state.SetChannelId(7001)
	state.SetLastMessageId(7100)
	state.SetLastReadMessageId(7099)
	state.SetMentionCount(2)
	messageReady := new(messagev1.GetUserReadyStateResponse)
	messageReady.SetDmChannels([]*messagev1.DmChannel{dm})
	messageReady.SetReadStates([]*messagev1.ChannelReadState{state})
	messageClient := &fakeMessage{response: messageReady}
	server.svcCtx.MessageClient = messageClient

	identify := new(sessionv1.Identify)
	identify.SetToken("token")
	session, err := server.identify(t.Context(), "conn-ready", "gateway-a", "gen-a", identify)
	require.NoError(t, err)
	require.Equal(t, []int64{7001}, messageClient.request.GetGuildChannelIds())

	var payload readyPayload
	require.NoError(t, json.Unmarshal([]byte(session.replay[0].frame.GetJsonPayload()), &payload))
	require.Equal(t, "9001", payload.Guilds[0].ID)
	require.Equal(t, int64(11), payload.Guilds[0].ChannelLayoutRevision)
	require.Equal(t, "Community description", payload.Guilds[0].Description)
	require.Equal(t, "42", payload.Guilds[0].Roles[0].Permissions)
	require.Equal(t, []string{"9002"}, payload.Guilds[0].MemberRoleIDs)
	require.Equal(t, "7001", payload.Guilds[0].Channels[0].ID)
	require.Equal(t, "7001", payload.Guilds[0].PermissionOverwrites[0].ChannelID)
	require.Equal(t, "1024", payload.Guilds[0].PermissionOverwrites[0].Allow)
	require.Equal(t, "1002", payload.DmChannels[0].RecipientID)
	require.Equal(t, "1002", payload.DmChannels[0].Recipient.UserID)
	require.Equal(t, "User 1002", payload.DmChannels[0].Recipient.Name)
	require.Equal(t, "7100", payload.ReadStates[0].LastMessageID)
	require.Equal(t, "7099", payload.ReadStates[0].LastReadMessageID)
	require.Equal(t, int32(2), payload.ReadStates[0].MentionCount)
	require.Equal(t, readyPresencePreference{Status: "online", Version: "1"}, payload.PresencePreference)
	require.Equal(t, []readyPresence{
		{UserID: "1001", Status: int32(presencev1.PresenceStatus_PRESENCE_STATUS_OFFLINE), Version: "1002"},
		{UserID: "1002", Status: int32(presencev1.PresenceStatus_PRESENCE_STATUS_OFFLINE), Version: "1003"},
	}, payload.Presences)
}

func TestGetReadyPresencesUsesBoundedBatches(t *testing.T) {
	server := newTestServer()
	client := new(recordingPresence)
	server.svcCtx.PresenceClient = client
	profiles := make(map[int64]*userv1.UserProfile, 501)
	for i := range 501 {
		userID := int64(2000 + i)
		profile := new(userv1.UserProfile)
		profile.SetUserId(userID)
		profiles[userID] = profile
	}

	presences, err := server.getReadyPresences(t.Context(), 1001, profiles)
	require.NoError(t, err)
	require.Len(t, presences, 502)
	require.Len(t, client.requests, 2)
	require.Len(t, client.requests[0], 500)
	require.Len(t, client.requests[1], 2)
	require.Equal(t, int64(1001), client.requests[0][0])
}

func TestIdentifyQueuesEventsAfterReady(t *testing.T) {
	server := newTestServer()
	started := make(chan struct{})
	release := make(chan struct{})
	server.svcCtx.MessageClient = &fakeMessage{started: started, release: release}
	identify := new(sessionv1.Identify)
	identify.SetToken("token")

	type result struct {
		session *logicalSession
		err     error
	}
	resultCh := make(chan result, 1)
	go func() {
		session, err := server.identify(t.Context(), "conn-buffer", "gateway-a", "gen-a", identify)
		resultCh <- result{session: session, err: err}
	}()
	<-started

	event := new(sessionv1.EventEnvelope)
	event.SetType(realtime.EventMessageCreated)
	event.SetJsonPayload(`{"channel_id":"7001"}`)
	event.SetIdempotencyKey(1)
	req := new(sessionv1.DispatchUserEventRequest)
	req.SetUserId(1001)
	req.SetEvent(event)
	resp, err := server.DispatchUserEvent(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int32(1), resp.GetDelivered())
	close(release)

	identified := <-resultCh
	require.NoError(t, identified.err)
	require.Len(t, identified.session.replay, 2)
	require.Equal(t, realtime.GatewayEventReady, identified.session.replay[0].frame.GetType())
	require.Equal(t, realtime.EventMessageCreated, identified.session.replay[1].frame.GetType())
}

func TestIdentifyFailsWhenPendingDispatchLimitIsExceeded(t *testing.T) {
	server := newTestServer()
	server.svcCtx.Cfg.Node.MaxPendingDispatches = 1
	started := make(chan struct{})
	release := make(chan struct{})
	server.svcCtx.MessageClient = &fakeMessage{started: started, release: release}
	identify := new(sessionv1.Identify)
	identify.SetToken("token")

	type result struct {
		session *logicalSession
		err     error
	}
	resultCh := make(chan result, 1)
	go func() {
		session, err := server.identify(t.Context(), "conn-overflow", "gateway-a", "gen-a", identify)
		resultCh <- result{session: session, err: err}
	}()
	<-started

	event := new(sessionv1.EventEnvelope)
	event.SetType(realtime.EventMessageCreated)
	event.SetJsonPayload(`{"channel_id":"7001"}`)
	event.SetIdempotencyKey(1)
	req := new(sessionv1.DispatchUserEventRequest)
	req.SetUserId(1001)
	req.SetEvent(event)
	first, err := server.DispatchUserEvent(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int32(1), first.GetDelivered())
	event.SetIdempotencyKey(2)
	second, err := server.DispatchUserEvent(t.Context(), req)
	require.NoError(t, err)
	require.Zero(t, second.GetDelivered())

	initializing := server.userSessions(1001)
	require.Len(t, initializing, 1)
	initializing[0].mu.Lock()
	require.True(t, initializing[0].pendingDispatchOverflow)
	require.Empty(t, initializing[0].pendingDispatches)
	initializing[0].mu.Unlock()

	close(release)
	identified := <-resultCh
	require.Nil(t, identified.session)
	require.Equal(t, codes.ResourceExhausted, status.Code(identified.err))
	require.Empty(t, server.userSessions(1001))
}

func TestPendingDispatchByteLimitMarksInitializationOverflow(t *testing.T) {
	server := newTestServer()
	server.svcCtx.Cfg.Node.MaxPendingDispatches = 10
	server.svcCtx.Cfg.Node.MaxPendingDispatchBytes = 3
	session := testLogicalSession(1001, 9001)
	session.initializing = true

	require.False(t, server.dispatchSession(session, realtime.EventMessageCreated, []byte("four")))
	require.True(t, session.pendingDispatchOverflow)
	require.Empty(t, session.pendingDispatches)
}
