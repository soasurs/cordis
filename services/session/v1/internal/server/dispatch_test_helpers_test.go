package server

import (
	"strconv"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
)

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

func userEventRequest(userID int64, eventType, payload string) *sessionv1.DispatchUserEventRequest {
	event := new(sessionv1.EventEnvelope)
	event.SetType(eventType)
	event.SetJsonPayload(payload)
	event.SetIdempotencyKey(100)
	req := new(sessionv1.DispatchUserEventRequest)
	req.SetUserId(userID)
	req.SetEvent(event)
	return req
}
