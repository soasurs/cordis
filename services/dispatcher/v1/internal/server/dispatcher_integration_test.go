//go:build integration

package server

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/pkg/sessionregistry"
	"github.com/soasurs/cordis/services/dispatcher/v1/config"
	"github.com/soasurs/cordis/services/dispatcher/v1/internal/discovery"
)

// TestDispatcherIntegration shares Kafka, Redis, and etcd containers across
// all subtests; each subtest uses run-scoped topics, consumer groups, etcd
// prefixes, and route IDs.
func TestDispatcherIntegration(t *testing.T) {
	kafka := testkit.StartKafka(t)
	redisContainer := testkit.StartRedis(t)
	etcd := testkit.StartEtcd(t)
	rds, err := redis.NewRedis(redis.RedisConf{Host: redisContainer.Address, Type: redis.NodeType})
	require.NoError(t, err)
	env := &dispatcherEnv{kafkaAddress: kafka.Address, rds: rds, etcdHosts: []string{etcd.Address}}

	t.Run("guild message route", func(t *testing.T) { testGuildMessageRoute(t, env) })
	t.Run("guild route merges user route nodes", func(t *testing.T) { testGuildRouteMergesUserNodes(t, env) })
	t.Run("retry preserves uncommitted offset", func(t *testing.T) { testRetryPreservesUncommittedOffset(t, env) })
	t.Run("poison pill does not block partition", func(t *testing.T) { testPoisonPillDoesNotBlockPartition(t, env) })
	t.Run("user route", func(t *testing.T) { testUserRoute(t, env) })
	t.Run("presence fan-out", func(t *testing.T) { testPresenceFanOut(t, env) })
	t.Run("presence friend lookup retry", func(t *testing.T) { testPresenceFriendLookupRetry(t, env) })
}

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

type dispatcherEnv struct {
	kafkaAddress string
	rds          *redis.Redis
	etcdHosts    []string
}

type dispatcherHarness struct {
	env           *dispatcherEnv
	runID         string
	guildTopic    string
	messageTopic  string
	userTopic     string
	presenceTopic string
	consumerGroup string
	producer      *kgo.Client
	registry      *sessionregistry.EtcdDirectory
	userClient    *fakeDispatcherUserClient
}

func newHarness(t *testing.T, env *dispatcherEnv) *dispatcherHarness {
	t.Helper()
	runID := strconv.FormatInt(time.Now().UnixNano(), 10)
	h := &dispatcherHarness{
		env:           env,
		runID:         runID,
		guildTopic:    "cordis.integration.guild." + runID,
		messageTopic:  "cordis.integration.message." + runID,
		userTopic:     "cordis.integration.user." + runID,
		presenceTopic: "cordis.integration.presence." + runID,
		consumerGroup: "cordis.integration.dispatcher." + runID,
		userClient:    newFakeDispatcherUserClient(),
	}

	producer, err := kgo.NewClient(kgo.SeedBrokers(env.kafkaAddress))
	require.NoError(t, err)
	t.Cleanup(producer.Close)
	h.producer = producer
	testkit.CreateKafkaTopic(t, producer, h.guildTopic)
	testkit.CreateKafkaTopic(t, producer, h.messageTopic)
	testkit.CreateKafkaTopic(t, producer, h.userTopic)
	testkit.CreateKafkaTopic(t, producer, h.presenceTopic)

	registry, err := sessionregistry.New(sessionregistry.Config{
		Hosts:              env.etcdHosts,
		Prefix:             "/cordis/integration/" + runID,
		DialTimeoutSeconds: 5,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, registry.Close()) })
	h.registry = registry
	return h
}

func (h *dispatcherHarness) registerNode(t *testing.T, nodeID, generation, address string) {
	t.Helper()
	registry, err := sessionregistry.New(sessionregistry.Config{
		Hosts:              h.env.etcdHosts,
		Prefix:             "/cordis/integration/" + h.runID,
		DialTimeoutSeconds: 5,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, registry.Close()) })
	require.NoError(t, registry.Register(t.Context(), sessionregistry.Node{
		ID:         nodeID,
		Generation: generation,
		RPCAddress: address,
		Status:     sessionregistry.StatusReady,
	}, time.Minute))
}

func (h *dispatcherHarness) addRoute(t *testing.T, kind discovery.RouteKind, id int64, nodeID, generation string) {
	t.Helper()
	key := fmt.Sprintf("gateway:routes:%s:{%d}:nodes", kind, id)
	expiresAt := time.Now().Add(time.Minute).UnixMilli()
	_, err := h.env.rds.ZaddCtx(t.Context(), key, expiresAt, nodeID+"\x1f"+generation)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = h.env.rds.DelCtx(ctx, key)
	})
}

func (h *dispatcherHarness) startDispatcher(t *testing.T) {
	t.Helper()
	dispatcher := New(config.Config{
		Kafka: config.KafkaConfig{
			Seeds:         []string{h.env.kafkaAddress},
			GuildTopic:    h.guildTopic,
			MessageTopic:  h.messageTopic,
			UserTopic:     h.userTopic,
			PresenceTopic: h.presenceTopic,
			ConsumerGroup: h.consumerGroup,
		},
		Dispatcher: config.DispatcherConfig{
			DispatchTimeoutSeconds: 5,
			RetryMinMilliseconds:   10,
			RetryMaxSeconds:        1,
		},
	}, discovery.NewRedisResolver(h.env.rds, h.registry), h.userClient)

	runCtx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		dispatcher.Run(runCtx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("dispatcher did not stop")
		}
	})
}

func (h *dispatcherHarness) produce(t *testing.T, topic, key, value string) {
	t.Helper()
	require.NoError(t, h.producer.ProduceSync(t.Context(), &kgo.Record{
		Topic: topic,
		Key:   []byte(key),
		Value: []byte(value),
	}).FirstErr())
}

// committedOffset returns the committed offset of partition 0 for the
// harness consumer group, or -1 when nothing has been committed.
func (h *dispatcherHarness) committedOffset(t *testing.T, topic string) int64 {
	t.Helper()
	req := kmsg.NewPtrOffsetFetchRequest()
	req.Group = h.consumerGroup
	legacyTopic := kmsg.NewOffsetFetchRequestTopic()
	legacyTopic.Topic = topic
	legacyTopic.Partitions = []int32{0}
	req.Topics = append(req.Topics, legacyTopic)
	reqGroup := kmsg.NewOffsetFetchRequestGroup()
	reqGroup.Group = h.consumerGroup
	groupTopic := kmsg.NewOffsetFetchRequestGroupTopic()
	groupTopic.Topic = topic
	groupTopic.Partitions = []int32{0}
	reqGroup.Topics = append(reqGroup.Topics, groupTopic)
	req.Groups = append(req.Groups, reqGroup)

	resp, err := req.RequestWith(t.Context(), h.producer)
	require.NoError(t, err)
	for _, group := range resp.Groups {
		for _, respTopic := range group.Topics {
			for _, partition := range respTopic.Partitions {
				if respTopic.Topic == topic && partition.Partition == 0 {
					return partition.Offset
				}
			}
		}
	}
	for _, respTopic := range resp.Topics {
		for _, partition := range respTopic.Partitions {
			if respTopic.Topic == topic && partition.Partition == 0 {
				return partition.Offset
			}
		}
	}
	return -1
}

func startSessionServer(t *testing.T, server sessionv1.SessionServiceServer) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	grpcServer := grpc.NewServer()
	sessionv1.RegisterSessionServiceServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)
	return listener.Addr().String()
}

type recordingSessionServer struct {
	sessionv1.UnimplementedSessionServiceServer

	mu             sync.Mutex
	channelFailing bool
	channelCount   int
	guildCount     int
	userCount      int
	channelEvents  chan *sessionv1.DispatchGuildMessageEventRequest
	guildEventsCh  chan *sessionv1.DispatchGuildEventRequest
	userEventsCh   chan *sessionv1.DispatchUserEventRequest
}

func newRecordingSessionServer() *recordingSessionServer {
	return &recordingSessionServer{
		channelEvents: make(chan *sessionv1.DispatchGuildMessageEventRequest, 16),
		guildEventsCh: make(chan *sessionv1.DispatchGuildEventRequest, 16),
		userEventsCh:  make(chan *sessionv1.DispatchUserEventRequest, 16),
	}
}

func (s *recordingSessionServer) DispatchGuildMessageEvent(
	_ context.Context,
	req *sessionv1.DispatchGuildMessageEventRequest,
) (*sessionv1.DispatchGuildMessageEventResponse, error) {
	s.mu.Lock()
	s.channelCount++
	failing := s.channelFailing
	s.mu.Unlock()
	if failing {
		return nil, status.Error(codes.Unavailable, "injected failure")
	}
	s.channelEvents <- req
	return new(sessionv1.DispatchGuildMessageEventResponse), nil
}

func (s *recordingSessionServer) DispatchGuildEvent(
	_ context.Context,
	req *sessionv1.DispatchGuildEventRequest,
) (*sessionv1.DispatchGuildEventResponse, error) {
	s.mu.Lock()
	s.guildCount++
	s.mu.Unlock()
	s.guildEventsCh <- req
	return new(sessionv1.DispatchGuildEventResponse), nil
}

func (s *recordingSessionServer) DispatchUserEvent(
	_ context.Context,
	req *sessionv1.DispatchUserEventRequest,
) (*sessionv1.DispatchUserEventResponse, error) {
	s.mu.Lock()
	s.userCount++
	s.mu.Unlock()
	s.userEventsCh <- req
	return new(sessionv1.DispatchUserEventResponse), nil
}

func (s *recordingSessionServer) setChannelFailing(failing bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.channelFailing = failing
}

func (s *recordingSessionServer) channelCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.channelCount
}

func (s *recordingSessionServer) waitChannelEvent(t *testing.T) *sessionv1.DispatchGuildMessageEventRequest {
	t.Helper()
	select {
	case request := <-s.channelEvents:
		return request
	case <-time.After(30 * time.Second):
		t.Fatal("session node did not receive the channel event")
		return nil
	}
}

func (s *recordingSessionServer) guildCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.guildCount
}

func (s *recordingSessionServer) waitGuildEvent(t *testing.T) *sessionv1.DispatchGuildEventRequest {
	t.Helper()
	select {
	case request := <-s.guildEventsCh:
		return request
	case <-time.After(30 * time.Second):
		t.Fatal("session node did not receive the guild event")
		return nil
	}
}

func (s *recordingSessionServer) userCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.userCount
}

func (s *recordingSessionServer) waitUserEvent(t *testing.T) *sessionv1.DispatchUserEventRequest {
	t.Helper()
	select {
	case request := <-s.userEventsCh:
		return request
	case <-time.After(30 * time.Second):
		t.Fatal("session node did not receive the user event")
		return nil
	}
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

// fakeDispatcherUserClient serves friend lists for presence fan-out tests.
type fakeDispatcherUserClient struct {
	userv1.UserServiceClient
	mu      sync.Mutex
	friends map[int64][]int64
	err     error
	calls   int
}

func newFakeDispatcherUserClient() *fakeDispatcherUserClient {
	return &fakeDispatcherUserClient{friends: make(map[int64][]int64)}
}

func (f *fakeDispatcherUserClient) setFriends(userID int64, friendIDs ...int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.friends[userID] = friendIDs
}

func (f *fakeDispatcherUserClient) setError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeDispatcherUserClient) relationshipCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeDispatcherUserClient) ListRelationships(_ context.Context, req *userv1.ListRelationshipsRequest, _ ...grpc.CallOption) (*userv1.ListRelationshipsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	resp := new(userv1.ListRelationshipsResponse)
	if req.GetBeforeTargetId() != 0 {
		// Single page is enough for the harness.
		return resp, nil
	}
	var values []*userv1.Relationship
	for _, friendID := range f.friends[req.GetUserId()] {
		row := new(userv1.Relationship)
		row.SetUserId(req.GetUserId())
		row.SetTargetId(friendID)
		row.SetType(userv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND)
		values = append(values, row)
	}
	resp.SetRelationships(values)
	if len(values) > 0 {
		resp.SetBeforeTargetId(values[len(values)-1].GetTargetId())
	}
	return resp, nil
}
