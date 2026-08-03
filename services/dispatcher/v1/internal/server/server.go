package server

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	cordiskafka "github.com/soasurs/cordis/pkg/kafka"
	"github.com/soasurs/cordis/pkg/kafka/partitionconsumer"
	"github.com/soasurs/cordis/pkg/observability"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/dispatcher/v1/config"
	"github.com/soasurs/cordis/services/dispatcher/v1/internal/discovery"
)

const presenceDispatchConcurrency = 16

type eventEnvelope struct {
	Type           string          `json:"t"`
	Data           json.RawMessage `json:"d"`
	IdempotencyKey string          `json:"idempotency_key"`
	StreamSequence int64           `json:"stream_sequence,omitempty"`
	DeliveryIndex  int32           `json:"delivery_index,omitempty"`
}

type eventRouting struct {
	ID        eventID   `json:"id"`
	GuildID   eventID   `json:"guild_id"`
	ChannelID eventID   `json:"channel_id"`
	UserID    eventID   `json:"user_id"`
	OwnerID   eventID   `json:"owner_id"`
	GuildIDs  []eventID `json:"guild_ids"`
}

type eventID int64

func (id *eventID) UnmarshalJSON(value []byte) error {
	if len(value) == 0 || string(value) == "null" {
		*id = 0
		return nil
	}
	if value[0] == '"' {
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return err
		}
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return err
		}
		*id = eventID(parsed)
		return nil
	}
	parsed, err := strconv.ParseInt(string(value), 10, 64)
	if err != nil {
		return err
	}
	*id = eventID(parsed)
	return nil
}

type Server struct {
	cfg           config.Config
	consumers     []eventConsumer
	resolver      discovery.Resolver
	userClient    userv1.UserServiceClient
	guildClient   guildv1.GuildServiceClient
	messageClient messagev1.MessageServiceClient
	tracer        trace.Tracer

	mu      sync.Mutex
	clients map[string]sessionv1.SessionServiceClient
	conns   map[string]*grpc.ClientConn
}

type eventConsumer struct {
	client *partitionconsumer.Consumer
}

func New(
	cfg config.Config,
	resolver discovery.Resolver,
	userClient userv1.UserServiceClient,
	guildClient guildv1.GuildServiceClient,
	messageClient messagev1.MessageServiceClient,
) *Server {
	cfg.Dispatcher = cfg.Dispatcher.WithDefaults()
	if len(cfg.Kafka.Seeds) == 0 {
		panic("dispatcher kafka seeds are required")
	}
	specs := [][2]string{
		{
			defaultString(cfg.Kafka.GuildTopic, "cordis.guild.events.v1"),
			defaultString(cfg.Kafka.GuildConsumerGroup, "cordis.dispatcher.guild.v1"),
		},
		{
			defaultString(cfg.Kafka.MessageTopic, "cordis.message.events.v1"),
			defaultString(cfg.Kafka.MessageConsumerGroup, "cordis.dispatcher.message.v1"),
		},
		{
			defaultString(cfg.Kafka.UserTopic, "cordis.user.events.v1"),
			defaultString(cfg.Kafka.UserConsumerGroup, "cordis.dispatcher.user.v1"),
		},
		{
			defaultString(cfg.Kafka.PresenceTopic, "cordis.presence.events.v1"),
			defaultString(cfg.Kafka.PresenceConsumerGroup, "cordis.dispatcher.presence.v1"),
		},
	}
	seenTopics := make(map[string]struct{}, len(specs))
	seenGroups := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if _, exists := seenTopics[spec[0]]; exists {
			panic("dispatcher kafka topics must be unique")
		}
		if _, exists := seenGroups[spec[1]]; exists {
			panic("dispatcher kafka consumer groups must be unique")
		}
		seenTopics[spec[0]] = struct{}{}
		seenGroups[spec[1]] = struct{}{}
	}
	server := &Server{
		cfg: cfg, consumers: make([]eventConsumer, 0, len(specs)), resolver: resolver,
		userClient: userClient, guildClient: guildClient, messageClient: messageClient,
		clients: make(map[string]sessionv1.SessionServiceClient),
		conns:   make(map[string]*grpc.ClientConn),
		tracer:  otel.Tracer(observability.DispatcherInstrumentationName),
	}
	for _, spec := range specs {
		consumer, err := partitionconsumer.New(
			partitionconsumer.Config{
				MaxPollRecords:        cfg.Dispatcher.MaxPollRecords,
				QueueSize:             cfg.Dispatcher.PartitionQueueSize,
				CommitInterval:        commitInterval(cfg.Dispatcher.CommitIntervalMilliseconds),
				MaxUncommittedRecords: cfg.Dispatcher.MaxUncommittedRecords,
				RevokeTimeout:         server.revokeTimeout(),
				CommitTimeout:         server.dispatchTimeout(),
				ShutdownTimeout:       cfg.ShutdownDuration(),
				RetryMin:              server.retryMin(),
				RetryMax:              server.retryMax(),
			},
			func(ctx context.Context, record *kgo.Record) (bool, error) {
				permanent, err := server.dispatchRecord(ctx, record)
				return !permanent, err
			},
			kgo.SeedBrokers(cfg.Kafka.Seeds...),
			kgo.ConsumerGroup(spec[1]),
			kgo.ConsumeTopics(spec[0]),
		)
		if err != nil {
			for _, created := range server.consumers {
				created.client.Close()
			}
			panic(err)
		}
		server.consumers = append(server.consumers, eventConsumer{client: consumer})
	}
	return server
}

func (s *Server) Run(ctx context.Context) {
	defer s.close()
	var wg sync.WaitGroup
	for _, consumer := range s.consumers {
		wg.Go(func() { consumer.client.Run(ctx) })
	}
	wg.Wait()
}

func (s *Server) dispatchRecord(ctx context.Context, record *kgo.Record) (permanent bool, err error) {
	topic := ""
	partition := int32(0)
	if record != nil {
		topic = record.Topic
		partition = record.Partition
	}
	tracer := s.tracer
	if tracer == nil {
		tracer = otel.Tracer(observability.DispatcherInstrumentationName)
	}
	ctx, span := tracer.Start(
		cordiskafka.ExtractTraceContext(ctx, record),
		"process "+topic,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.destination.name", topic),
			attribute.String("messaging.consumer.group.name", s.consumerGroup(record)),
			attribute.Int64("messaging.destination.partition.id", int64(partition)),
			attribute.String("messaging.operation.name", "process"),
			attribute.String("messaging.operation.type", "process"),
		),
	)
	defer func() {
		result := "success"
		if err != nil {
			result = "retryable_failure"
			errorType := "dispatch"
			if permanent {
				result = "permanent_failure"
				errorType = "invalid_event"
			}
			span.SetAttributes(attribute.String("error.type", errorType))
			span.SetStatus(codes.Error, result)
		}
		span.SetAttributes(attribute.String("cordis.messaging.result", result))
		span.End()
	}()

	if record == nil {
		return true, errors.New("record is nil")
	}
	var event eventEnvelope
	if err := json.Unmarshal(record.Value, &event); err != nil {
		return true, err
	}
	if strings.TrimSpace(event.Type) == "" || !json.Valid(event.Data) {
		return true, errors.New("invalid event envelope")
	}
	if isCanonicalEventType(event.Type) {
		span.SetAttributes(attribute.String("cordis.event.type", event.Type))
	}
	var routing eventRouting
	if err := json.Unmarshal(event.Data, &routing); err != nil {
		return true, err
	}

	idempotencyKey, err := parseIdempotencyKey(event.IdempotencyKey)
	if err != nil {
		return true, err
	}

	switch event.Type {
	case realtime.EventMessageCreated, realtime.EventMessageUpdated, realtime.EventMessageDeleted,
		realtime.EventMessageReadUpdated:
		channelID := int64(routing.ChannelID)
		if channelID <= 0 {
			return true, errors.New("message event channel id is invalid")
		}
		guildID := int64(routing.GuildID)
		userID := int64(routing.UserID)
		switch {
		case guildID > 0 && userID == 0:
			return false, s.dispatchGuildMessage(ctx, guildID, channelID, event, idempotencyKey)
		case userID > 0 && guildID == 0:
			return false, s.dispatchUser(ctx, userID, event, idempotencyKey)
		case guildID == 0 && userID == 0:
			return true, errors.New("message event aggregate route is missing")
		default:
			return true, errors.New("message event aggregate route is invalid")
		}
	default:
		if event.Type == realtime.EventPresencePreferenceUpdated {
			userID := int64(routing.UserID)
			if userID <= 0 {
				return true, errors.New("presence preference event user id is invalid")
			}
			return false, s.dispatchUser(ctx, userID, event, idempotencyKey)
		}
		if event.Type == realtime.EventPresenceUpdated {
			userID := int64(routing.UserID)
			if userID <= 0 {
				return true, errors.New("presence event user id is invalid")
			}
			return false, s.dispatchPresence(ctx, userID, event, routing, idempotencyKey)
		}
		if strings.HasPrefix(event.Type, "presence.") {
			return true, errors.New("unsupported presence event type")
		}
		// relationship.* and dm.* records are user-routed: the payload
		// user_id names the recipient.
		if strings.HasPrefix(event.Type, "relationship.") || strings.HasPrefix(event.Type, "dm.") {
			userID := int64(routing.UserID)
			if userID <= 0 {
				return true, errors.New("user-routed event user id is invalid")
			}
			return false, s.dispatchUser(ctx, userID, event, idempotencyKey)
		}
		if event.Type == realtime.EventUserProfileUpdated {
			userID := int64(routing.UserID)
			if userID <= 0 {
				return true, errors.New("profile event user id is invalid")
			}
			return false, s.dispatchUserProfile(ctx, userID, event, idempotencyKey)
		}
		if !strings.HasPrefix(event.Type, "guild.") {
			return true, errors.New("unsupported event type")
		}
		guildID := int64(routing.GuildID)
		if guildID == 0 &&
			(event.Type == realtime.EventGuildCreated || event.Type == realtime.EventGuildUpdated || event.Type == realtime.EventGuildDeleted) {
			guildID = int64(routing.ID)
		}
		if guildID <= 0 {
			return true, errors.New("guild event guild id is invalid")
		}
		if event.Type == realtime.EventGuildCreated && routing.OwnerID <= 0 {
			return true, errors.New("guild created owner id is invalid")
		}
		if event.Type == realtime.EventGuildMemberJoined && routing.UserID <= 0 {
			return true, errors.New("guild member joined user id is invalid")
		}
		return false, s.dispatchGuild(ctx, guildID, event, routing, idempotencyKey)
	}
}

func parseIdempotencyKey(value string) (int64, error) {
	idempotencyKey, err := strconv.ParseInt(value, 10, 64)
	if err != nil || idempotencyKey <= 0 || strconv.FormatInt(idempotencyKey, 10) != value {
		return 0, errors.New("invalid idempotency key")
	}
	return idempotencyKey, nil
}

// dispatchGuildMessage uses the aggregate Guild route to locate candidate
// Session nodes. Each node filters recipients through its visibility snapshots.
func (s *Server) dispatchGuildMessage(ctx context.Context, guildID, channelID int64, event eventEnvelope, idempotencyKey int64) error {
	nodes, err := s.resolver.Resolve(ctx, discovery.RouteGuild, guildID)
	if err != nil {
		return err
	}
	return s.forEachNode(ctx, nodes, func(ctx context.Context, client sessionv1.SessionServiceClient) error {
		req := new(sessionv1.DispatchGuildMessageEventRequest)
		req.SetGuildId(guildID)
		req.SetChannelId(channelID)
		req.SetEvent(protoEvent(event, idempotencyKey))
		_, err := client.DispatchGuildMessageEvent(ctx, req)
		return err
	})
}

// dispatchUser fans a user-routed event out to the recipient's session
// nodes only.
func (s *Server) dispatchUser(ctx context.Context, userID int64, event eventEnvelope, idempotencyKey int64) error {
	nodes, err := s.resolver.Resolve(ctx, discovery.RouteUser, userID)
	if err != nil {
		return err
	}
	return s.forEachNode(ctx, nodes, func(ctx context.Context, client sessionv1.SessionServiceClient) error {
		req := new(sessionv1.DispatchUserEventRequest)
		req.SetUserId(userID)
		req.SetEvent(protoEvent(event, idempotencyKey))
		_, err := client.DispatchUserEvent(ctx, req)
		return err
	})
}

// dispatchPresence fans a presence transition out along two paths: the
// user's guilds (shared-guild members) and their friends plus their own
// other devices. A recipient reachable through both paths receives the
// event more than once; presence updates are idempotent state, so
// duplicates are harmless.
func (s *Server) dispatchPresence(ctx context.Context, userID int64, event eventEnvelope, routing eventRouting, idempotencyKey int64) error {
	friends, err := s.friendIDs(ctx, userID)
	if err != nil {
		return err
	}

	seenGuilds := make(map[int64]struct{}, len(routing.GuildIDs))
	guildIDs := make([]int64, 0, len(routing.GuildIDs))
	for _, rawGuildID := range routing.GuildIDs {
		guildID := int64(rawGuildID)
		if guildID <= 0 {
			continue
		}
		if _, ok := seenGuilds[guildID]; ok {
			continue
		}
		seenGuilds[guildID] = struct{}{}
		guildIDs = append(guildIDs, guildID)
	}

	seenRecipients := make(map[int64]struct{}, len(friends)+1)
	recipientIDs := make([]int64, 0, len(friends)+1)
	for _, friendID := range friends {
		if friendID <= 0 || friendID == userID {
			continue
		}
		if _, ok := seenRecipients[friendID]; ok {
			continue
		}
		seenRecipients[friendID] = struct{}{}
		recipientIDs = append(recipientIDs, friendID)
	}
	friendCount := len(recipientIDs)
	recipientIDs = append(recipientIDs, userID)

	logx.WithContext(ctx).Infow("presence fan-out", logx.Field("user_id", userID), logx.Field("guild_count", len(guildIDs)), logx.Field("friend_count", friendCount), logx.Field("total_recipients", len(recipientIDs)))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(presenceDispatchConcurrency)
	for i := range max(len(guildIDs), len(recipientIDs)) {
		if i < len(guildIDs) {
			guildID := guildIDs[i]
			group.Go(func() error {
				nodes, err := s.resolver.Resolve(groupCtx, discovery.RouteGuild, guildID)
				if err != nil {
					return err
				}
				return s.forEachNode(groupCtx, nodes, func(ctx context.Context, client sessionv1.SessionServiceClient) error {
					req := new(sessionv1.DispatchGuildEventRequest)
					req.SetGuildId(guildID)
					req.SetEvent(protoEvent(event, idempotencyKey))
					_, err := client.DispatchGuildEvent(ctx, req)
					return err
				})
			})
		}
		if i < len(recipientIDs) {
			recipientID := recipientIDs[i]
			group.Go(func() error { return s.dispatchUser(groupCtx, recipientID, event, idempotencyKey) })
		}
	}
	return group.Wait()
}

// friendIDs pages through the user's friendships.
func (s *Server) friendIDs(ctx context.Context, userID int64) ([]int64, error) {
	var friends []int64
	var cursor string
	for {
		req := new(userv1.ListRelationshipsRequest)
		req.SetUserId(userID)
		req.SetType(userv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND)
		if cursor != "" {
			req.SetCursor(cursor)
		}
		req.SetLimit(200)
		resp, err := s.userClient.ListRelationships(ctx, req)
		if err != nil {
			return nil, err
		}
		relationships := resp.GetRelationships()
		if len(relationships) == 0 {
			return friends, nil
		}
		for _, relationship := range relationships {
			friends = append(friends, relationship.GetTargetId())
		}
		if !resp.HasNextCursor() {
			return friends, nil
		}
		cursor = resp.GetNextCursor()
	}
}

func (s *Server) dispatchGuild(ctx context.Context, guildID int64, event eventEnvelope, routing eventRouting, idempotencyKey int64) error {
	nodes, err := s.resolver.Resolve(ctx, discovery.RouteGuild, guildID)
	if err != nil {
		return err
	}
	if event.Type == realtime.EventGuildCreated || event.Type == realtime.EventGuildMemberJoined {
		userID := int64(routing.OwnerID)
		if event.Type == realtime.EventGuildMemberJoined {
			userID = int64(routing.UserID)
		}
		userNodes, err := s.resolver.Resolve(ctx, discovery.RouteUser, userID)
		if err != nil {
			return err
		}
		nodes = mergeNodes(nodes, userNodes)
	}
	return s.forEachNode(ctx, nodes, func(ctx context.Context, client sessionv1.SessionServiceClient) error {
		req := new(sessionv1.DispatchGuildEventRequest)
		req.SetGuildId(guildID)
		req.SetEvent(protoEvent(event, idempotencyKey))
		_, err := client.DispatchGuildEvent(ctx, req)
		return err
	})
}

func (s *Server) forEachNode(
	ctx context.Context,
	nodes []discovery.Node,
	call func(context.Context, sessionv1.SessionServiceClient) error,
) error {
	for _, node := range nodes {
		client, err := s.client(node.RPCAddress)
		if err != nil {
			return err
		}
		callCtx, cancel := context.WithTimeout(ctx, s.dispatchTimeout())
		err = call(callCtx, client)
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) client(address string) (sessionv1.SessionServiceClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if client := s.clients[address]; client != nil {
		return client, nil
	}
	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return nil, err
	}
	client := sessionv1.NewSessionServiceClient(conn)
	s.conns[address] = conn
	s.clients[address] = client
	return client, nil
}

func isCanonicalEventType(eventType string) bool {
	switch eventType {
	case realtime.EventGuildCreated,
		realtime.EventGuildUpdated,
		realtime.EventGuildDeleted,
		realtime.EventGuildMemberJoined,
		realtime.EventGuildMemberUpdated,
		realtime.EventGuildMemberRemoved,
		realtime.EventGuildMemberBanned,
		realtime.EventGuildMemberUnbanned,
		realtime.EventGuildRoleCreated,
		realtime.EventGuildRoleUpdated,
		realtime.EventGuildRoleDeleted,
		realtime.EventGuildMemberRolesUpdated,
		realtime.EventGuildChannelCreated,
		realtime.EventGuildChannelUpdated,
		realtime.EventGuildChannelDeleted,
		realtime.EventGuildChannelOverwriteUpdated,
		realtime.EventGuildChannelOverwriteDeleted,
		realtime.EventMessageCreated,
		realtime.EventMessageUpdated,
		realtime.EventMessageDeleted,
		realtime.EventMessageReadUpdated,
		realtime.EventRelationshipUpdated,
		realtime.EventRelationshipRemoved,
		realtime.EventUserProfileUpdated,
		realtime.EventDmChannelCreated,
		realtime.EventPresenceUpdated,
		realtime.EventPresencePreferenceUpdated:
		return true
	default:
		return false
	}
}

func (s *Server) close() {
	for _, consumer := range s.consumers {
		consumer.client.Close()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, conn := range s.conns {
		_ = conn.Close()
	}
}

func (s *Server) consumerGroup(record *kgo.Record) string {
	if record == nil {
		return ""
	}
	switch record.Topic {
	case defaultString(s.cfg.Kafka.GuildTopic, "cordis.guild.events.v1"):
		return defaultString(s.cfg.Kafka.GuildConsumerGroup, "cordis.dispatcher.guild.v1")
	case defaultString(s.cfg.Kafka.MessageTopic, "cordis.message.events.v1"):
		return defaultString(s.cfg.Kafka.MessageConsumerGroup, "cordis.dispatcher.message.v1")
	case defaultString(s.cfg.Kafka.UserTopic, "cordis.user.events.v1"):
		return defaultString(s.cfg.Kafka.UserConsumerGroup, "cordis.dispatcher.user.v1")
	case defaultString(s.cfg.Kafka.PresenceTopic, "cordis.presence.events.v1"):
		return defaultString(s.cfg.Kafka.PresenceConsumerGroup, "cordis.dispatcher.presence.v1")
	default:
		return ""
	}
}

func (s *Server) dispatchTimeout() time.Duration {
	if s.cfg.Dispatcher.DispatchTimeoutSeconds <= 0 {
		return time.Duration(config.DefaultDispatchTimeoutSeconds) * time.Second
	}
	return time.Duration(s.cfg.Dispatcher.DispatchTimeoutSeconds) * time.Second
}

func (s *Server) revokeTimeout() time.Duration {
	if s.cfg.Dispatcher.RevokeTimeoutSeconds <= 0 {
		return time.Duration(config.DefaultRevokeTimeoutSeconds) * time.Second
	}
	return time.Duration(s.cfg.Dispatcher.RevokeTimeoutSeconds) * time.Second
}

func (s *Server) retryMin() time.Duration {
	if s.cfg.Dispatcher.RetryMinMilliseconds <= 0 {
		return time.Duration(config.DefaultRetryMinMilliseconds) * time.Millisecond
	}
	return time.Duration(s.cfg.Dispatcher.RetryMinMilliseconds) * time.Millisecond
}

func (s *Server) retryMax() time.Duration {
	if s.cfg.Dispatcher.RetryMaxSeconds <= 0 {
		return time.Duration(config.DefaultRetryMaxSeconds) * time.Second
	}
	return time.Duration(s.cfg.Dispatcher.RetryMaxSeconds) * time.Second
}

func commitInterval(milliseconds int) time.Duration {
	if milliseconds <= 0 {
		return time.Duration(config.DefaultCommitIntervalMilliseconds) * time.Millisecond
	}
	return time.Duration(milliseconds) * time.Millisecond
}

func protoEvent(event eventEnvelope, idempotencyKey int64) *sessionv1.EventEnvelope {
	result := new(sessionv1.EventEnvelope)
	result.SetType(event.Type)
	result.SetJsonPayload(string(event.Data))
	result.SetIdempotencyKey(idempotencyKey)
	result.SetStreamSequence(event.StreamSequence)
	result.SetDeliveryIndex(event.DeliveryIndex)
	return result
}

func mergeNodes(groups ...[]discovery.Node) []discovery.Node {
	seen := make(map[string]struct{})
	var result []discovery.Node
	for _, group := range groups {
		for _, node := range group {
			key := node.ID + "\x1f" + node.Generation
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, node)
		}
	}
	return result
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
