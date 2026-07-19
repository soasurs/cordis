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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/dispatcher/v1/config"
	"github.com/soasurs/cordis/services/dispatcher/v1/internal/discovery"
)

type eventEnvelope struct {
	Type string          `json:"t"`
	Data json.RawMessage `json:"d"`
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
	cfg        config.Config
	consumer   *kgo.Client
	resolver   discovery.Resolver
	userClient userv1.UserServiceClient

	mu      sync.Mutex
	clients map[string]sessionv1.SessionServiceClient
	conns   map[string]*grpc.ClientConn
}

func New(cfg config.Config, resolver discovery.Resolver, userClient userv1.UserServiceClient) *Server {
	if len(cfg.Kafka.Seeds) == 0 {
		panic("dispatcher kafka seeds are required")
	}
	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Kafka.Seeds...),
		kgo.ConsumerGroup(defaultString(cfg.Kafka.ConsumerGroup, "cordis.dispatcher.v1")),
		kgo.ConsumeTopics(
			defaultString(cfg.Kafka.GuildTopic, "cordis.guild.events.v1"),
			defaultString(cfg.Kafka.MessageTopic, "cordis.message.events.v1"),
			defaultString(cfg.Kafka.UserTopic, "cordis.user.events.v1"),
			defaultString(cfg.Kafka.PresenceTopic, "cordis.presence.events.v1"),
		),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		panic(err)
	}
	return &Server{
		cfg: cfg, consumer: consumer, resolver: resolver, userClient: userClient,
		clients: make(map[string]sessionv1.SessionServiceClient),
		conns:   make(map[string]*grpc.ClientConn),
	}
}

func (s *Server) Run(ctx context.Context) {
	defer s.close()
	for {
		records := s.consumer.PollRecords(ctx, 1)
		if ctx.Err() != nil {
			return
		}
		for _, fetchErr := range records.Errors() {
			logx.WithContext(ctx).Errorw("poll dispatcher event",
				logx.Field("topic", fetchErr.Topic),
				logx.Field("partition", fetchErr.Partition),
				logx.Field("error", fetchErr.Err),
			)
		}
		for _, record := range records.Records() {
			permanent, err := s.dispatchRecord(ctx, record)
			if err != nil && !permanent {
				s.retryRecord(ctx, record)
				continue
			}
			if err != nil {
				logx.WithContext(ctx).Errorw("drop invalid dispatcher event",
					logx.Field("topic", record.Topic),
					logx.Field("partition", record.Partition),
					logx.Field("offset", record.Offset),
					logx.Field("error", err),
				)
			}
			if err := s.consumer.CommitRecords(ctx, record); err != nil && ctx.Err() == nil {
				logx.WithContext(ctx).Errorw("commit dispatcher event", logx.Field("error", err))
			}
		}
	}
}

func (s *Server) retryRecord(ctx context.Context, record *kgo.Record) {
	delay := s.retryMin()
	for ctx.Err() == nil {
		logx.WithContext(ctx).Errorw("retry dispatcher event",
			logx.Field("topic", record.Topic),
			logx.Field("partition", record.Partition),
			logx.Field("offset", record.Offset),
			logx.Field("retry_after", delay),
		)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		permanent, err := s.dispatchRecord(ctx, record)
		if err == nil || permanent {
			if err := s.consumer.CommitRecords(ctx, record); err != nil && ctx.Err() == nil {
				logx.WithContext(ctx).Errorw("commit retried dispatcher event", logx.Field("error", err))
			}
			return
		}
		delay = min(delay*2, s.retryMax())
	}
}

func (s *Server) dispatchRecord(ctx context.Context, record *kgo.Record) (bool, error) {
	var event eventEnvelope
	if err := json.Unmarshal(record.Value, &event); err != nil {
		return true, err
	}
	if strings.TrimSpace(event.Type) == "" || !json.Valid(event.Data) {
		return true, errors.New("invalid event envelope")
	}
	var routing eventRouting
	if err := json.Unmarshal(event.Data, &routing); err != nil {
		return true, err
	}

	switch event.Type {
	case realtime.EventMessageCreated, realtime.EventMessageUpdated, realtime.EventMessageDeleted:
		channelID := int64(routing.ChannelID)
		if channelID <= 0 {
			return true, errors.New("message event channel id is invalid")
		}
		guildID := int64(routing.GuildID)
		userID := int64(routing.UserID)
		switch {
		case guildID > 0 && userID == 0:
			return false, s.dispatchGuildMessage(ctx, guildID, channelID, event)
		case userID > 0 && guildID == 0:
			return false, s.dispatchUser(ctx, userID, event)
		case guildID == 0 && userID == 0:
			return true, errors.New("message event aggregate route is missing")
		default:
			return true, errors.New("message event aggregate route is invalid")
		}
	default:
		if strings.HasPrefix(event.Type, "presence.") {
			userID := int64(routing.UserID)
			if userID <= 0 {
				return true, errors.New("presence event user id is invalid")
			}
			return false, s.dispatchPresence(ctx, userID, event, routing)
		}
		// relationship.* and dm.* records are user-routed: the payload
		// user_id names the recipient.
		if strings.HasPrefix(event.Type, "relationship.") || strings.HasPrefix(event.Type, "dm.") {
			userID := int64(routing.UserID)
			if userID <= 0 {
				return true, errors.New("user-routed event user id is invalid")
			}
			return false, s.dispatchUser(ctx, userID, event)
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
		return false, s.dispatchGuild(ctx, guildID, event, routing)
	}
}

// dispatchGuildMessage uses the aggregate Guild route to locate candidate
// Session nodes. Each node filters recipients through its visibility snapshots.
func (s *Server) dispatchGuildMessage(ctx context.Context, guildID, channelID int64, event eventEnvelope) error {
	nodes, err := s.resolver.Resolve(ctx, discovery.RouteGuild, guildID)
	if err != nil {
		return err
	}
	return s.forEachNode(ctx, nodes, func(ctx context.Context, client sessionv1.SessionServiceClient) error {
		req := new(sessionv1.DispatchGuildMessageEventRequest)
		req.SetGuildId(guildID)
		req.SetChannelId(channelID)
		req.SetEvent(protoEvent(event))
		_, err := client.DispatchGuildMessageEvent(ctx, req)
		return err
	})
}

// dispatchUser fans a user-routed event out to the recipient's session
// nodes only.
func (s *Server) dispatchUser(ctx context.Context, userID int64, event eventEnvelope) error {
	nodes, err := s.resolver.Resolve(ctx, discovery.RouteUser, userID)
	if err != nil {
		return err
	}
	return s.forEachNode(ctx, nodes, func(ctx context.Context, client sessionv1.SessionServiceClient) error {
		req := new(sessionv1.DispatchUserEventRequest)
		req.SetUserId(userID)
		req.SetEvent(protoEvent(event))
		_, err := client.DispatchUserEvent(ctx, req)
		return err
	})
}

// dispatchPresence fans a presence transition out along two paths: the
// user's guilds (shared-guild members) and their friends plus their own
// other devices. A recipient reachable through both paths receives the
// event more than once; presence updates are idempotent state, so
// duplicates are harmless.
func (s *Server) dispatchPresence(ctx context.Context, userID int64, event eventEnvelope, routing eventRouting) error {
	for _, rawGuildID := range routing.GuildIDs {
		guildID := int64(rawGuildID)
		if guildID <= 0 {
			continue
		}
		nodes, err := s.resolver.Resolve(ctx, discovery.RouteGuild, guildID)
		if err != nil {
			return err
		}
		if err := s.forEachNode(ctx, nodes, func(ctx context.Context, client sessionv1.SessionServiceClient) error {
			req := new(sessionv1.DispatchGuildEventRequest)
			req.SetGuildId(guildID)
			req.SetEvent(protoEvent(event))
			_, err := client.DispatchGuildEvent(ctx, req)
			return err
		}); err != nil {
			return err
		}
	}

	recipients, err := s.friendIDs(ctx, userID)
	if err != nil {
		return err
	}
	// The user's own other devices track the transition too.
	recipients = append(recipients, userID)
	logx.WithContext(ctx).Infow("presence fan-out", logx.Field("user_id", userID), logx.Field("guild_count", len(routing.GuildIDs)), logx.Field("friend_count", len(recipients)-1), logx.Field("total_recipients", len(recipients)))
	for _, recipientID := range recipients {
		logx.WithContext(ctx).Infow("dispatch user presence", logx.Field("recipient_id", recipientID))
		if err := s.dispatchUser(ctx, recipientID, event); err != nil {
			return err
		}
	}
	return nil
}

// friendIDs pages through the user's friendships.
func (s *Server) friendIDs(ctx context.Context, userID int64) ([]int64, error) {
	var friends []int64
	var before int64
	for {
		req := new(userv1.ListRelationshipsRequest)
		req.SetUserId(userID)
		req.SetType(userv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND)
		req.SetBeforeTargetId(before)
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
		before = resp.GetBeforeTargetId()
	}
}

func (s *Server) dispatchGuild(ctx context.Context, guildID int64, event eventEnvelope, routing eventRouting) error {
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
		req.SetEvent(protoEvent(event))
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
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	client := sessionv1.NewSessionServiceClient(conn)
	s.conns[address] = conn
	s.clients[address] = client
	return client, nil
}

func (s *Server) close() {
	s.consumer.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, conn := range s.conns {
		_ = conn.Close()
	}
}

func (s *Server) dispatchTimeout() time.Duration {
	if s.cfg.Dispatcher.DispatchTimeoutSeconds <= 0 {
		return 5 * time.Second
	}
	return time.Duration(s.cfg.Dispatcher.DispatchTimeoutSeconds) * time.Second
}

func (s *Server) retryMin() time.Duration {
	if s.cfg.Dispatcher.RetryMinMilliseconds <= 0 {
		return 100 * time.Millisecond
	}
	return time.Duration(s.cfg.Dispatcher.RetryMinMilliseconds) * time.Millisecond
}

func (s *Server) retryMax() time.Duration {
	if s.cfg.Dispatcher.RetryMaxSeconds <= 0 {
		return 5 * time.Second
	}
	return time.Duration(s.cfg.Dispatcher.RetryMaxSeconds) * time.Second
}

func protoEvent(event eventEnvelope) *sessionv1.EventEnvelope {
	result := new(sessionv1.EventEnvelope)
	result.SetType(event.Type)
	result.SetJsonPayload(string(event.Data))
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
