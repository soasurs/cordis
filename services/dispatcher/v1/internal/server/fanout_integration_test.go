//go:build integration

package server

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/dispatcher/v1/internal/discovery"
)

func testPresenceFanOut(t *testing.T, env *dispatcherEnv) {
	const (
		userID   = int64(7501)
		friendID = int64(7502)
		guildA   = int64(7503)
		guildB   = int64(7504)
	)
	h := newHarness(t, env)
	h.userClient.setFriends(userID, friendID, friendID, userID)
	node := newRecordingSessionServer()
	address := startSessionServer(t, node)
	h.registerNode(t, "session-a", "generation-1", address)
	h.addRoute(t, discovery.RouteGuild, guildA, "session-a", "generation-1")
	h.addRoute(t, discovery.RouteGuild, guildB, "session-a", "generation-1")
	h.addRoute(t, discovery.RouteUser, userID, "session-a", "generation-1")
	h.addRoute(t, discovery.RouteUser, friendID, "session-a", "generation-1")
	h.startDispatcher(t)

	h.produce(t, h.presenceTopic, strconv.FormatInt(userID, 10),
		`{"t":"`+realtime.EventPresenceUpdated+`","d":{"user_id":"7501","status":1,"guild_ids":["7503","7503","0","7504"]},"idempotency_key":"1012"}`)

	guildRequestA := node.waitGuildEvent(t)
	guildRequestB := node.waitGuildEvent(t)
	userRequestA := node.waitUserEvent(t)
	userRequestB := node.waitUserEvent(t)
	guildIDs := []int64{guildRequestA.GetGuildId(), guildRequestB.GetGuildId()}
	userIDs := []int64{userRequestA.GetUserId(), userRequestB.GetUserId()}
	require.ElementsMatch(t, []int64{guildA, guildB}, guildIDs)
	require.ElementsMatch(t, []int64{userID, friendID}, userIDs)
	for _, request := range []*sessionv1.EventEnvelope{
		guildRequestA.GetEvent(), guildRequestB.GetEvent(), userRequestA.GetEvent(), userRequestB.GetEvent(),
	} {
		require.Equal(t, int64(1012), request.GetIdempotencyKey())
	}
	require.Eventually(t, func() bool { return h.committedOffset(t, h.presenceTopic) == 1 },
		15*time.Second, 50*time.Millisecond, "presence offset must be committed after both paths succeed")
	time.Sleep(500 * time.Millisecond)
	require.Equal(t, 2, node.guildCalls(), "duplicate Guild routes must be dispatched once")
	require.Equal(t, 2, node.userCalls(), "duplicate friend and self routes must be dispatched once")
}

func testPresenceFriendLookupRetry(t *testing.T, env *dispatcherEnv) {
	const (
		userID  = int64(7601)
		guildID = int64(7602)
	)
	h := newHarness(t, env)
	h.userClient.setError(status.Error(codes.Unavailable, "injected failure"))
	node := newRecordingSessionServer()
	address := startSessionServer(t, node)
	h.registerNode(t, "session-a", "generation-1", address)
	h.addRoute(t, discovery.RouteGuild, guildID, "session-a", "generation-1")
	h.addRoute(t, discovery.RouteUser, userID, "session-a", "generation-1")
	h.startDispatcher(t)

	h.produce(t, h.presenceTopic, strconv.FormatInt(userID, 10),
		`{"t":"`+realtime.EventPresenceUpdated+`","d":{"user_id":"7601","status":1,"guild_ids":["7602"]},"idempotency_key":"1013"}`)

	require.Eventually(t, func() bool { return h.userClient.relationshipCalls() >= 2 },
		15*time.Second, 20*time.Millisecond, "dispatcher did not retry the failed friend lookup")
	require.Equal(t, 0, node.guildCalls(), "Guild delivery must wait for friend lookup")
	require.Equal(t, int64(-1), h.committedOffset(t, h.presenceTopic),
		"offset must stay uncommitted while friend lookup fails")

	h.userClient.setError(nil)
	require.Equal(t, guildID, node.waitGuildEvent(t).GetGuildId())
	require.Equal(t, userID, node.waitUserEvent(t).GetUserId())
	require.Eventually(t, func() bool { return h.committedOffset(t, h.presenceTopic) == 1 },
		15*time.Second, 50*time.Millisecond, "presence offset must commit after friend lookup recovers")
}

func testProfileFanOut(t *testing.T, env *dispatcherEnv) {
	const (
		userID   = int64(7701)
		friendID = int64(7702)
		guildID  = int64(7703)
		dmPeerID = int64(7704)
	)
	h := newHarness(t, env)
	h.userClient.setFriends(userID, friendID)
	h.guildClient = staticGuildClient{guildIDs: []int64{guildID}}
	h.messageClient = staticMessageClient{recipientIDs: []int64{dmPeerID}}
	node := newRecordingSessionServer()
	address := startSessionServer(t, node)
	h.registerNode(t, "session-a", "generation-1", address)
	h.addRoute(t, discovery.RouteGuild, guildID, "session-a", "generation-1")
	for _, recipientID := range []int64{userID, friendID, dmPeerID} {
		h.addRoute(t, discovery.RouteUser, recipientID, "session-a", "generation-1")
	}
	h.startDispatcher(t)

	h.produce(t, h.userTopic, strconv.FormatInt(userID, 10),
		`{"t":"`+realtime.EventUserProfileUpdated+`","d":{"user_id":"7701","username":"updated","name":"Updated","avatar_asset_id":"8801","created_at":1,"updated_at":2},"idempotency_key":"1014"}`)

	guildRequest := node.waitGuildEvent(t)
	require.Equal(t, guildID, guildRequest.GetGuildId())
	require.Equal(t, realtime.EventUserProfileUpdated, guildRequest.GetEvent().GetType())
	userRequests := []*sessionv1.DispatchUserEventRequest{
		node.waitUserEvent(t),
		node.waitUserEvent(t),
		node.waitUserEvent(t),
	}
	require.ElementsMatch(t, []int64{userID, friendID, dmPeerID}, []int64{
		userRequests[0].GetUserId(),
		userRequests[1].GetUserId(),
		userRequests[2].GetUserId(),
	})
	require.Eventually(t, func() bool { return h.committedOffset(t, h.userTopic) == 1 },
		15*time.Second, 50*time.Millisecond, "profile offset must commit after every audience path succeeds")
}

// fakeDispatcherUserClient serves relationship lists for fan-out tests.
