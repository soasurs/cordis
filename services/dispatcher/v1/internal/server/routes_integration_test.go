//go:build integration

package server

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/dispatcher/v1/internal/discovery"
)

func testGuildMessageRoute(t *testing.T, env *dispatcherEnv) {
	const guildID = int64(7000)
	h := newHarness(t, env)
	node := newRecordingSessionServer()
	address := startSessionServer(t, node)
	h.registerNode(t, "session-a", "generation-1", address)
	h.addRoute(t, discovery.RouteGuild, guildID, "session-a", "generation-1")
	h.startDispatcher(t)

	h.produce(t, h.messageTopic, strconv.FormatInt(guildID, 10),
		`{"t":"`+realtime.EventMessageCreated+`","d":{"id":"9001","guild_id":"7000","channel_id":"7001"},"idempotency_key":"1001"}`)

	request := node.waitChannelEvent(t)
	require.Equal(t, int64(7001), request.GetChannelId())
	require.Equal(t, guildID, request.GetGuildId())
	require.Equal(t, realtime.EventMessageCreated, request.GetEvent().GetType())
	require.Equal(t, int64(1001), request.GetEvent().GetIdempotencyKey())
	require.JSONEq(t, `{"id":"9001","guild_id":"7000","channel_id":"7001"}`, request.GetEvent().GetJsonPayload())
}

func testGuildRouteMergesUserNodes(t *testing.T, env *dispatcherEnv) {
	const (
		guildID = int64(7101)
		userID  = int64(7102)
	)
	h := newHarness(t, env)
	nodeA := newRecordingSessionServer()
	nodeB := newRecordingSessionServer()
	h.registerNode(t, "session-a", "generation-1", startSessionServer(t, nodeA))
	h.registerNode(t, "session-b", "generation-1", startSessionServer(t, nodeB))
	h.addRoute(t, discovery.RouteGuild, guildID, "session-a", "generation-1")
	h.addRoute(t, discovery.RouteUser, userID, "session-a", "generation-1")
	h.addRoute(t, discovery.RouteUser, userID, "session-b", "generation-1")
	h.startDispatcher(t)

	h.produce(t, h.guildTopic, strconv.FormatInt(guildID, 10),
		`{"t":"`+realtime.EventGuildMemberJoined+`","d":{"guild_id":"7101","user_id":"7102"},"idempotency_key":"1002"}`)

	requestA := nodeA.waitGuildEvent(t)
	require.Equal(t, guildID, requestA.GetGuildId())
	require.Equal(t, realtime.EventGuildMemberJoined, requestA.GetEvent().GetType())
	require.Equal(t, int64(1002), requestA.GetEvent().GetIdempotencyKey())
	requestB := nodeB.waitGuildEvent(t)
	require.Equal(t, guildID, requestB.GetGuildId())

	h.produce(t, h.guildTopic, strconv.FormatInt(guildID, 10),
		`{"t":"`+realtime.EventGuildUpdated+`","d":{"id":"7101","name":"Cordis"},"idempotency_key":"1003"}`)
	updated := nodeA.waitGuildEvent(t)
	require.Equal(t, realtime.EventGuildUpdated, updated.GetEvent().GetType())

	time.Sleep(500 * time.Millisecond)
	require.Equal(t, 2, nodeA.guildCalls(), "guild-route node must be deduplicated per dispatch")
	require.Equal(t, 1, nodeB.guildCalls(), "user-only node must not receive plain guild events")
}

func testUserRoute(t *testing.T, env *dispatcherEnv) {
	const userID = int64(7401)
	h := newHarness(t, env)
	node := newRecordingSessionServer()
	address := startSessionServer(t, node)
	h.registerNode(t, "session-a", "generation-1", address)
	h.addRoute(t, discovery.RouteUser, userID, "session-a", "generation-1")
	h.startDispatcher(t)

	h.produce(t, h.userTopic, strconv.FormatInt(userID, 10),
		`{"t":"`+realtime.EventRelationshipUpdated+`","d":{"user_id":"7401","target_id":"8001","type":3,"created_at":1,"updated_at":0},"idempotency_key":"1006"}`)

	request := node.waitUserEvent(t)
	require.Equal(t, userID, request.GetUserId())
	require.Equal(t, realtime.EventRelationshipUpdated, request.GetEvent().GetType())
	require.Equal(t, int64(1006), request.GetEvent().GetIdempotencyKey())
	require.JSONEq(t, `{"user_id":"7401","target_id":"8001","type":3,"created_at":1,"updated_at":0}`, request.GetEvent().GetJsonPayload())

	h.produce(t, h.userTopic, strconv.FormatInt(userID, 10),
		`{"t":"`+realtime.EventRelationshipRemoved+`","d":{"user_id":"7401","target_id":"8001"},"idempotency_key":"1007"}`)

	request = node.waitUserEvent(t)
	require.Equal(t, userID, request.GetUserId())
	require.Equal(t, realtime.EventRelationshipRemoved, request.GetEvent().GetType())
	require.JSONEq(t, `{"user_id":"7401","target_id":"8001"}`, request.GetEvent().GetJsonPayload())

	h.produce(t, h.userTopic, "poison", `{"t":"relationship.updated","d":{"target_id":"8001"},"idempotency_key":"1008"}`)
	h.produce(t, h.userTopic, strconv.FormatInt(userID, 10),
		`{"t":"`+realtime.EventRelationshipUpdated+`","d":{"user_id":"7401","target_id":"8001"},"idempotency_key":"1009"}`)

	request = node.waitUserEvent(t)
	require.Equal(t, userID, request.GetUserId())

	require.Eventually(t, func() bool { return h.committedOffset(t, h.userTopic) == 4 },
		15*time.Second, 50*time.Millisecond, "poison record must be dropped and committed")
	require.Equal(t, 3, node.userCalls(),
		"poison record without user_id must not reach the session node")

	// dm.channel.created arrives on the message topic but is user-routed.
	h.produce(t, h.messageTopic, strconv.FormatInt(userID, 10),
		`{"t":"`+realtime.EventDmChannelCreated+`","d":{"channel_id":"9001","user_id":"7401","recipient_id":"8001","created_at":1},"idempotency_key":"1010"}`)

	request = node.waitUserEvent(t)
	require.Equal(t, userID, request.GetUserId())
	require.Equal(t, realtime.EventDmChannelCreated, request.GetEvent().GetType())
	require.JSONEq(t, `{"channel_id":"9001","user_id":"7401","recipient_id":"8001","created_at":1}`, request.GetEvent().GetJsonPayload())

	h.produce(t, h.messageTopic, strconv.FormatInt(userID, 10),
		`{"t":"`+realtime.EventMessageCreated+`","d":{"id":"9101","channel_id":"9001","user_id":"7401"},"idempotency_key":"1011"}`)

	request = node.waitUserEvent(t)
	require.Equal(t, userID, request.GetUserId())
	require.Equal(t, realtime.EventMessageCreated, request.GetEvent().GetType())
}
