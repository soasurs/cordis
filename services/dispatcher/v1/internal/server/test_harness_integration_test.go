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

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/sessionregistry"
	"github.com/soasurs/cordis/services/dispatcher/v1/config"
	"github.com/soasurs/cordis/services/dispatcher/v1/internal/discovery"
)

type dispatcherEnv struct {
	kafkaAddress string
	rds          *redis.Redis
	etcdHosts    []string
}

type dispatcherHarness struct {
	env            *dispatcherEnv
	runID          string
	guildTopic     string
	messageTopic   string
	userTopic      string
	presenceTopic  string
	consumerGroups map[string]string
	producer       *kgo.Client
	registry       *sessionregistry.EtcdDirectory
	userClient     *fakeDispatcherUserClient
	guildClient    guildv1.GuildServiceClient
	messageClient  messagev1.MessageServiceClient
}

func newHarness(t *testing.T, env *dispatcherEnv) *dispatcherHarness {
	return newPartitionedHarness(t, env, 1)
}

func newPartitionedHarness(t *testing.T, env *dispatcherEnv, messagePartitions int32) *dispatcherHarness {
	t.Helper()
	runID := strconv.FormatInt(time.Now().UnixNano(), 10)
	h := &dispatcherHarness{
		env:           env,
		runID:         runID,
		guildTopic:    "cordis.integration.guild." + runID,
		messageTopic:  "cordis.integration.message." + runID,
		userTopic:     "cordis.integration.user." + runID,
		presenceTopic: "cordis.integration.presence." + runID,
		consumerGroups: map[string]string{
			"guild":    "cordis.integration.dispatcher.guild." + runID,
			"message":  "cordis.integration.dispatcher.message." + runID,
			"user":     "cordis.integration.dispatcher.user." + runID,
			"presence": "cordis.integration.dispatcher.presence." + runID,
		},
		userClient:    newFakeDispatcherUserClient(),
		guildClient:   emptyGuildClient{},
		messageClient: emptyMessageClient{},
	}

	producer, err := kgo.NewClient(
		kgo.SeedBrokers(env.kafkaAddress),
		kgo.RecordPartitioner(kgo.ManualPartitioner()),
	)
	require.NoError(t, err)
	t.Cleanup(producer.Close)
	h.producer = producer
	testkit.CreateKafkaTopic(t, producer, h.guildTopic)
	testkit.CreateKafkaTopicWithPartitions(t, producer, h.messageTopic, messagePartitions)
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
	h.startDispatcherWithConfig(t, config.DispatcherConfig{
		DispatchTimeoutSeconds:     5,
		RetryMinMilliseconds:       10,
		RetryMaxSeconds:            1,
		CommitIntervalMilliseconds: 100,
	})
}

func (h *dispatcherHarness) startDispatcherWithConfig(t *testing.T, dispatcherConfig config.DispatcherConfig) (context.CancelFunc, <-chan struct{}) {
	t.Helper()
	dispatcher := New(config.Config{
		Kafka: config.KafkaConfig{
			Seeds:                 []string{h.env.kafkaAddress},
			GuildTopic:            h.guildTopic,
			MessageTopic:          h.messageTopic,
			UserTopic:             h.userTopic,
			PresenceTopic:         h.presenceTopic,
			GuildConsumerGroup:    h.consumerGroups["guild"],
			MessageConsumerGroup:  h.consumerGroups["message"],
			UserConsumerGroup:     h.consumerGroups["user"],
			PresenceConsumerGroup: h.consumerGroups["presence"],
		},
		Dispatcher: dispatcherConfig,
	},
		discovery.NewRedisResolver(h.env.rds, h.registry),
		h.userClient,
		h.guildClient,
		h.messageClient,
	)

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
	return cancel, done
}

func (h *dispatcherHarness) produce(t *testing.T, topic, key, value string) {
	t.Helper()
	require.NoError(t, h.producer.ProduceSync(t.Context(), &kgo.Record{
		Topic: topic,
		Key:   []byte(key),
		Value: []byte(value),
	}).FirstErr())
}

func (h *dispatcherHarness) producePartition(t *testing.T, topic string, partition int32, value string) {
	t.Helper()
	require.NoError(t, h.producer.ProduceSync(t.Context(), &kgo.Record{
		Topic:     topic,
		Partition: partition,
		Value:     []byte(value),
	}).FirstErr())
}

// committedOffset returns the committed offset of partition 0 for the
// harness topic consumer group, or -1 when nothing has been committed.

func (h *dispatcherHarness) committedOffset(t *testing.T, topic string) int64 {
	return h.committedOffsetForPartition(t, topic, 0)
}

func (h *dispatcherHarness) committedOffsetForPartition(t *testing.T, topic string, partitionID int32) int64 {
	t.Helper()
	group := h.consumerGroup(topic)
	req := kmsg.NewPtrOffsetFetchRequest()
	req.Group = group
	legacyTopic := kmsg.NewOffsetFetchRequestTopic()
	legacyTopic.Topic = topic
	legacyTopic.Partitions = []int32{partitionID}
	req.Topics = append(req.Topics, legacyTopic)
	reqGroup := kmsg.NewOffsetFetchRequestGroup()
	reqGroup.Group = group
	groupTopic := kmsg.NewOffsetFetchRequestGroupTopic()
	groupTopic.Topic = topic
	groupTopic.Partitions = []int32{partitionID}
	reqGroup.Topics = append(reqGroup.Topics, groupTopic)
	req.Groups = append(req.Groups, reqGroup)

	resp, err := req.RequestWith(t.Context(), h.producer)
	require.NoError(t, err)
	for _, group := range resp.Groups {
		for _, respTopic := range group.Topics {
			for _, value := range respTopic.Partitions {
				if respTopic.Topic == topic && value.Partition == partitionID {
					return value.Offset
				}
			}
		}
	}
	for _, respTopic := range resp.Topics {
		for _, value := range respTopic.Partitions {
			if respTopic.Topic == topic && value.Partition == partitionID {
				return value.Offset
			}
		}
	}
	return -1
}

func (h *dispatcherHarness) consumerGroup(topic string) string {
	switch topic {
	case h.guildTopic:
		return h.consumerGroups["guild"]
	case h.messageTopic:
		return h.consumerGroups["message"]
	case h.userTopic:
		return h.consumerGroups["user"]
	case h.presenceTopic:
		return h.consumerGroups["presence"]
	default:
		panic("unknown harness topic")
	}
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

	mu               sync.Mutex
	channelFailing   bool
	channelFails     map[int64]bool
	channelCount     int
	channelCallsByID map[int64]int
	guildCount       int
	userCount        int
	channelEvents    chan *sessionv1.DispatchGuildMessageEventRequest
	guildEventsCh    chan *sessionv1.DispatchGuildEventRequest
	userEventsCh     chan *sessionv1.DispatchUserEventRequest
}

type blockingRecordingSessionServer struct {
	*recordingSessionServer
	blockChannel int64
	started      chan struct{}
	release      chan struct{}
	startOnce    sync.Once
}

func (s *blockingRecordingSessionServer) DispatchGuildMessageEvent(
	ctx context.Context,
	req *sessionv1.DispatchGuildMessageEventRequest,
) (*sessionv1.DispatchGuildMessageEventResponse, error) {
	if req.GetChannelId() == s.blockChannel {
		s.startOnce.Do(func() { close(s.started) })
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, status.Error(codes.Canceled, "blocked dispatch canceled")
		}
	}
	return s.recordingSessionServer.DispatchGuildMessageEvent(ctx, req)
}

func newRecordingSessionServer() *recordingSessionServer {
	return &recordingSessionServer{
		channelFails:     make(map[int64]bool),
		channelCallsByID: make(map[int64]int),
		channelEvents:    make(chan *sessionv1.DispatchGuildMessageEventRequest, 16),
		guildEventsCh:    make(chan *sessionv1.DispatchGuildEventRequest, 16),
		userEventsCh:     make(chan *sessionv1.DispatchUserEventRequest, 16),
	}
}

func (s *recordingSessionServer) DispatchGuildMessageEvent(
	_ context.Context,
	req *sessionv1.DispatchGuildMessageEventRequest,
) (*sessionv1.DispatchGuildMessageEventResponse, error) {
	s.mu.Lock()
	s.channelCount++
	s.channelCallsByID[req.GetChannelId()]++
	failing := s.channelFailing || s.channelFails[req.GetChannelId()]
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

func (s *recordingSessionServer) setChannelFailingFor(channelID int64, failing bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.channelFails[channelID] = failing
}

func (s *recordingSessionServer) channelCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.channelCount
}

func (s *recordingSessionServer) channelCallsFor(channelID int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.channelCallsByID[channelID]
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

type fakeDispatcherUserClient struct {
	userv1.UserServiceClient
	mu      sync.Mutex
	friends map[int64][]int64
	err     error
	calls   int
}

type emptyGuildClient struct {
	guildv1.GuildServiceClient
}

func (emptyGuildClient) ListUserGuilds(
	context.Context,
	*guildv1.ListUserGuildsRequest,
	...grpc.CallOption,
) (*guildv1.ListUserGuildsResponse, error) {
	return new(guildv1.ListUserGuildsResponse), nil
}

type emptyMessageClient struct {
	messagev1.MessageServiceClient
}

func (emptyMessageClient) ListDmChannels(
	context.Context,
	*messagev1.ListDmChannelsRequest,
	...grpc.CallOption,
) (*messagev1.ListDmChannelsResponse, error) {
	return new(messagev1.ListDmChannelsResponse), nil
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
	if req.GetCursor() != "" {
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
	return resp, nil
}
