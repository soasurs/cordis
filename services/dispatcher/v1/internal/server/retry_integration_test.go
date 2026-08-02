//go:build integration

package server

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/dispatcher/v1/config"
	"github.com/soasurs/cordis/services/dispatcher/v1/internal/discovery"
)

func testRetryPreservesUncommittedOffset(t *testing.T, env *dispatcherEnv) {
	const guildID = int64(7200)
	h := newHarness(t, env)
	node := newRecordingSessionServer()
	node.setChannelFailing(true)
	h.registerNode(t, "session-a", "generation-1", startSessionServer(t, node))
	h.addRoute(t, discovery.RouteGuild, guildID, "session-a", "generation-1")
	h.startDispatcher(t)

	h.produce(t, h.messageTopic, strconv.FormatInt(guildID, 10),
		`{"t":"`+realtime.EventMessageCreated+`","d":{"id":"9001","guild_id":"7200","channel_id":"7201"},"idempotency_key":"1004"}`)

	require.Eventually(t, func() bool { return node.channelCalls() >= 2 },
		30*time.Second, 20*time.Millisecond, "dispatcher did not retry the failing dispatch")
	require.Equal(t, int64(-1), h.committedOffset(t, h.messageTopic),
		"offset must stay uncommitted while dispatch keeps failing")

	node.setChannelFailing(false)
	request := node.waitChannelEvent(t)
	require.Equal(t, int64(7201), request.GetChannelId())
	require.Eventually(t, func() bool { return h.committedOffset(t, h.messageTopic) == 1 },
		15*time.Second, 50*time.Millisecond, "offset must be committed after successful dispatch")
}

func testTopicRetryIsolation(t *testing.T, env *dispatcherEnv) {
	const (
		guildID = int64(7250)
		userID  = int64(7251)
	)
	h := newHarness(t, env)
	node := newRecordingSessionServer()
	node.setChannelFailing(true)
	h.registerNode(t, "session-a", "generation-1", startSessionServer(t, node))
	h.addRoute(t, discovery.RouteGuild, guildID, "session-a", "generation-1")
	h.addRoute(t, discovery.RouteUser, userID, "session-a", "generation-1")
	h.startDispatcher(t)

	h.produce(t, h.messageTopic, strconv.FormatInt(guildID, 10),
		`{"t":"`+realtime.EventMessageCreated+`","d":{"id":"9001","guild_id":"7250","channel_id":"7252"},"idempotency_key":"1015"}`)
	require.Eventually(t, func() bool { return node.channelCalls() >= 2 },
		15*time.Second, 20*time.Millisecond, "message consumer did not enter retry")

	h.produce(t, h.userTopic, strconv.FormatInt(userID, 10),
		`{"t":"`+realtime.EventRelationshipRemoved+`","d":{"user_id":"7251","target_id":"7253"},"idempotency_key":"1016"}`)

	require.Equal(t, userID, node.waitUserEvent(t).GetUserId())
	require.Equal(t, int64(-1), h.committedOffset(t, h.messageTopic))
	require.Eventually(t, func() bool { return h.committedOffset(t, h.userTopic) == 1 },
		15*time.Second, 50*time.Millisecond, "user topic must commit while message topic retries")
}

func testPartitionRetryIsolation(t *testing.T, env *dispatcherEnv) {
	const (
		failingGuildID = int64(7260)
		healthyGuildID = int64(7261)
		failingChannel = int64(7262)
		healthyChannel = int64(7263)
	)
	h := newPartitionedHarness(t, env, 2)
	node := newRecordingSessionServer()
	node.setChannelFailingFor(failingChannel, true)
	h.registerNode(t, "session-a", "generation-1", startSessionServer(t, node))
	h.addRoute(t, discovery.RouteGuild, failingGuildID, "session-a", "generation-1")
	h.addRoute(t, discovery.RouteGuild, healthyGuildID, "session-a", "generation-1")
	h.startDispatcher(t)

	h.producePartition(t, h.messageTopic, 0,
		`{"t":"`+realtime.EventMessageCreated+`","d":{"id":"9201","guild_id":"7260","channel_id":"7262"},"idempotency_key":"1017"}`)
	require.Eventually(t, func() bool { return node.channelCallsFor(failingChannel) >= 2 },
		15*time.Second, 20*time.Millisecond, "partition worker did not enter retry")

	h.producePartition(t, h.messageTopic, 1,
		`{"t":"`+realtime.EventMessageCreated+`","d":{"id":"9202","guild_id":"7261","channel_id":"7263"},"idempotency_key":"1018"}`)
	request := node.waitChannelEvent(t)
	require.Equal(t, healthyChannel, request.GetChannelId())
	require.Eventually(t, func() bool {
		return h.committedOffsetForPartition(t, h.messageTopic, 1) == 1
	}, 15*time.Second, 50*time.Millisecond, "healthy partition must commit while another partition retries")
	require.Equal(t, int64(-1), h.committedOffsetForPartition(t, h.messageTopic, 0))

	node.setChannelFailingFor(failingChannel, false)
	require.Equal(t, failingChannel, node.waitChannelEvent(t).GetChannelId())
	require.Eventually(t, func() bool {
		return h.committedOffsetForPartition(t, h.messageTopic, 0) == 1
	}, 15*time.Second, 50*time.Millisecond, "retried partition must commit after recovery")
}

func testRetrySurvivesRebalance(t *testing.T, env *dispatcherEnv) {
	const (
		guildID = int64(7270)
		channel = int64(7271)
	)
	h := newHarness(t, env)
	node := newRecordingSessionServer()
	node.setChannelFailingFor(channel, true)
	h.registerNode(t, "session-a", "generation-1", startSessionServer(t, node))
	h.addRoute(t, discovery.RouteGuild, guildID, "session-a", "generation-1")
	firstCancel, firstDone := h.startDispatcherWithConfig(t, config.DispatcherConfig{
		DispatchTimeoutSeconds:     5,
		RetryMinMilliseconds:       10,
		RetryMaxSeconds:            1,
		CommitIntervalMilliseconds: 100,
	})

	h.producePartition(t, h.messageTopic, 0,
		`{"t":"`+realtime.EventMessageCreated+`","d":{"id":"9203","guild_id":"7270","channel_id":"7271"},"idempotency_key":"1019"}`)
	require.Eventually(t, func() bool { return node.channelCallsFor(channel) >= 2 },
		15*time.Second, 20*time.Millisecond, "first dispatcher did not enter retry")

	secondCancel, secondDone := h.startDispatcherWithConfig(t, config.DispatcherConfig{
		DispatchTimeoutSeconds:     5,
		RetryMinMilliseconds:       10,
		RetryMaxSeconds:            1,
		CommitIntervalMilliseconds: 100,
	})
	firstCancel()
	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("first dispatcher did not stop")
	}
	require.Equal(t, int64(-1), h.committedOffset(t, h.messageTopic),
		"retrying record must not be committed during rebalance")

	node.setChannelFailingFor(channel, false)
	require.Equal(t, channel, node.waitChannelEvent(t).GetChannelId())
	require.Eventually(t, func() bool { return h.committedOffset(t, h.messageTopic) == 1 },
		15*time.Second, 50*time.Millisecond, "reassigned worker must commit after recovery")
	secondCancel()
	select {
	case <-secondDone:
	case <-time.After(5 * time.Second):
		t.Fatal("second dispatcher did not stop")
	}
}

func testQueuedRecordsReplayAfterRebalance(t *testing.T, env *dispatcherEnv) {
	const (
		firstGuild  = int64(7274)
		secondGuild = int64(7275)
		thirdGuild  = int64(7276)
		firstCh     = int64(7277)
		secondCh    = int64(7278)
		thirdCh     = int64(7279)
	)
	h := newHarness(t, env)
	node := newRecordingSessionServer()
	node.setChannelFailingFor(firstCh, true)
	h.registerNode(t, "session-a", "generation-1", startSessionServer(t, node))
	h.addRoute(t, discovery.RouteGuild, firstGuild, "session-a", "generation-1")
	h.addRoute(t, discovery.RouteGuild, secondGuild, "session-a", "generation-1")
	h.addRoute(t, discovery.RouteGuild, thirdGuild, "session-a", "generation-1")
	firstCancel, firstDone := h.startDispatcherWithConfig(t, config.DispatcherConfig{
		DispatchTimeoutSeconds:     5,
		RetryMinMilliseconds:       10,
		RetryMaxSeconds:            1,
		MaxPollRecords:             3,
		PartitionQueueSize:         1,
		CommitIntervalMilliseconds: 100,
	})

	h.producePartition(t, h.messageTopic, 0,
		`{"t":"`+realtime.EventMessageCreated+`","d":{"id":"9210","guild_id":"7274","channel_id":"7277"},"idempotency_key":"1026"}`)
	require.Eventually(t, func() bool { return node.channelCallsFor(firstCh) >= 1 },
		15*time.Second, 20*time.Millisecond, "first queued-replay record did not start")
	h.producePartition(t, h.messageTopic, 0,
		`{"t":"`+realtime.EventMessageCreated+`","d":{"id":"9211","guild_id":"7275","channel_id":"7278"},"idempotency_key":"1027"}`)
	h.producePartition(t, h.messageTopic, 0,
		`{"t":"`+realtime.EventMessageCreated+`","d":{"id":"9212","guild_id":"7276","channel_id":"7279"},"idempotency_key":"1028"}`)
	require.Eventually(t, func() bool { return node.channelCallsFor(firstCh) >= 2 },
		15*time.Second, 20*time.Millisecond, "first dispatcher did not enter queued-record retry")

	secondCancel, secondDone := h.startDispatcherWithConfig(t, config.DispatcherConfig{
		DispatchTimeoutSeconds:     5,
		RetryMinMilliseconds:       10,
		RetryMaxSeconds:            1,
		MaxPollRecords:             3,
		PartitionQueueSize:         1,
		CommitIntervalMilliseconds: 100,
	})
	firstCancel()
	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("first dispatcher did not stop")
	}
	require.Equal(t, int64(-1), h.committedOffset(t, h.messageTopic),
		"queued or retrying records must not be committed during rebalance")
	require.Equal(t, 0, node.channelCallsFor(secondCh),
		"records queued behind the retry must be discarded with the old worker")
	require.Equal(t, 0, node.channelCallsFor(thirdCh),
		"records queued behind the retry must be discarded with the old worker")

	node.setChannelFailingFor(firstCh, false)
	got := []int64{
		node.waitChannelEvent(t).GetChannelId(),
		node.waitChannelEvent(t).GetChannelId(),
		node.waitChannelEvent(t).GetChannelId(),
	}
	require.Equal(t, []int64{firstCh, secondCh, thirdCh}, got)
	require.Eventually(t, func() bool { return h.committedOffset(t, h.messageTopic) == 3 },
		15*time.Second, 50*time.Millisecond, "reassigned worker did not commit replayed records")
	secondCancel()
	select {
	case <-secondDone:
	case <-time.After(5 * time.Second):
		t.Fatal("second dispatcher did not stop")
	}
}

func testShutdownFlushesCompletedOffsets(t *testing.T, env *dispatcherEnv) {
	const (
		completedGuild = int64(7280)
		completedCh    = int64(7281)
		failingGuild   = int64(7282)
		failingCh      = int64(7283)
	)
	h := newPartitionedHarness(t, env, 2)
	node := newRecordingSessionServer()
	node.setChannelFailingFor(failingCh, true)
	h.registerNode(t, "session-a", "generation-1", startSessionServer(t, node))
	h.addRoute(t, discovery.RouteGuild, completedGuild, "session-a", "generation-1")
	h.addRoute(t, discovery.RouteGuild, failingGuild, "session-a", "generation-1")
	cancel, done := h.startDispatcherWithConfig(t, config.DispatcherConfig{
		DispatchTimeoutSeconds:     5,
		RetryMinMilliseconds:       10,
		RetryMaxSeconds:            1,
		CommitIntervalMilliseconds: 100,
	})

	h.producePartition(t, h.messageTopic, 0,
		`{"t":"`+realtime.EventMessageCreated+`","d":{"id":"9204","guild_id":"7280","channel_id":"7281"},"idempotency_key":"1020"}`)
	h.producePartition(t, h.messageTopic, 1,
		`{"t":"`+realtime.EventMessageCreated+`","d":{"id":"9205","guild_id":"7282","channel_id":"7283"},"idempotency_key":"1021"}`)
	require.Eventually(t, func() bool { return node.channelCallsFor(failingCh) >= 2 },
		15*time.Second, 20*time.Millisecond, "shutdown test did not enter retry")
	require.Equal(t, completedCh, node.waitChannelEvent(t).GetChannelId())

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("dispatcher did not stop")
	}
	require.Equal(t, int64(1), h.committedOffsetForPartition(t, h.messageTopic, 0),
		"shutdown must flush completed partition offsets")
	require.Equal(t, int64(-1), h.committedOffsetForPartition(t, h.messageTopic, 1),
		"shutdown must not commit the retrying record")
}

func testPartitionQueuePreservesOrder(t *testing.T, env *dispatcherEnv) {
	const (
		firstGuild  = int64(7290)
		secondGuild = int64(7291)
		thirdGuild  = int64(7292)
		firstCh     = int64(7293)
		secondCh    = int64(7294)
		thirdCh     = int64(7295)
	)
	h := newHarness(t, env)
	base := newRecordingSessionServer()
	node := &blockingRecordingSessionServer{
		recordingSessionServer: base,
		blockChannel:           firstCh,
		started:                make(chan struct{}),
		release:                make(chan struct{}),
	}
	h.registerNode(t, "session-a", "generation-1", startSessionServer(t, node))
	h.addRoute(t, discovery.RouteGuild, firstGuild, "session-a", "generation-1")
	h.addRoute(t, discovery.RouteGuild, secondGuild, "session-a", "generation-1")
	h.addRoute(t, discovery.RouteGuild, thirdGuild, "session-a", "generation-1")
	h.startDispatcherWithConfig(t, config.DispatcherConfig{
		DispatchTimeoutSeconds:     5,
		RetryMinMilliseconds:       10,
		RetryMaxSeconds:            1,
		MaxPollRecords:             3,
		PartitionQueueSize:         1,
		CommitIntervalMilliseconds: 100,
	})

	h.producePartition(t, h.messageTopic, 0,
		`{"t":"`+realtime.EventMessageCreated+`","d":{"id":"9206","guild_id":"7290","channel_id":"7293"},"idempotency_key":"1022"}`)
	require.Eventually(t, func() bool {
		select {
		case <-node.started:
			return true
		default:
			return false
		}
	}, 15*time.Second, 20*time.Millisecond, "first queue record did not start")
	h.producePartition(t, h.messageTopic, 0,
		`{"t":"`+realtime.EventMessageCreated+`","d":{"id":"9207","guild_id":"7291","channel_id":"7294"},"idempotency_key":"1023"}`)
	h.producePartition(t, h.messageTopic, 0,
		`{"t":"`+realtime.EventMessageCreated+`","d":{"id":"9208","guild_id":"7292","channel_id":"7295"},"idempotency_key":"1024"}`)
	close(node.release)

	got := []int64{
		node.waitChannelEvent(t).GetChannelId(),
		node.waitChannelEvent(t).GetChannelId(),
		node.waitChannelEvent(t).GetChannelId(),
	}
	require.Equal(t, []int64{firstCh, secondCh, thirdCh}, got)
}

func testPoisonPillDoesNotBlockPartition(t *testing.T, env *dispatcherEnv) {
	const guildID = int64(7300)
	h := newHarness(t, env)
	node := newRecordingSessionServer()
	h.registerNode(t, "session-a", "generation-1", startSessionServer(t, node))
	h.addRoute(t, discovery.RouteGuild, guildID, "session-a", "generation-1")
	h.startDispatcher(t)

	h.produce(t, h.messageTopic, "poison", `not-json`)
	h.produce(t, h.messageTopic, "poison", `{"t":"unsupported.event","d":{}}`)
	h.produce(t, h.messageTopic, strconv.FormatInt(guildID, 10),
		`{"t":"`+realtime.EventMessageCreated+`","d":{"id":"9001","guild_id":"7300","channel_id":"7301"},"idempotency_key":"1005"}`)

	request := node.waitChannelEvent(t)
	require.Equal(t, int64(7301), request.GetChannelId())
	require.Eventually(t, func() bool { return h.committedOffset(t, h.messageTopic) == 3 },
		15*time.Second, 50*time.Millisecond, "poison records must be dropped and committed")
	require.Equal(t, 1, node.channelCalls(),
		"poison records must not reach the session node")
}
