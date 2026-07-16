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
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/dispatcher/v1/config"
	"github.com/soasurs/cordis/services/dispatcher/v1/internal/discovery"
)

type eventEnvelope struct {
	Type string          `json:"t"`
	Data json.RawMessage `json:"d"`
}

type eventRouting struct {
	ID        string `json:"id"`
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
	OwnerID   string `json:"owner_id"`
}

type Server struct {
	cfg      config.Config
	consumer *kgo.Client
	resolver discovery.Resolver

	mu      sync.Mutex
	clients map[string]sessionv1.SessionServiceClient
	conns   map[string]*grpc.ClientConn
}

func New(cfg config.Config, resolver discovery.Resolver) *Server {
	if len(cfg.Kafka.Seeds) == 0 {
		panic("dispatcher kafka seeds are required")
	}
	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Kafka.Seeds...),
		kgo.ConsumerGroup(defaultString(cfg.Kafka.ConsumerGroup, "cordis.dispatcher.v1")),
		kgo.ConsumeTopics(
			defaultString(cfg.Kafka.GuildTopic, "cordis.guild.events.v1"),
			defaultString(cfg.Kafka.MessageTopic, "message.events"),
		),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		panic(err)
	}
	return &Server{
		cfg: cfg, consumer: consumer, resolver: resolver,
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
	case realtime.EventMessageCreated, realtime.EventMessageUpdated, realtime.EventMessageDeleted,
		realtime.EventReactionAdded, realtime.EventReactionRemoved:
		channelID := parseID(routing.ChannelID)
		if channelID <= 0 {
			return true, errors.New("message event channel id is invalid")
		}
		return false, s.dispatchChannel(ctx, channelID, event)
	default:
		if !strings.HasPrefix(event.Type, "guild.") {
			return true, errors.New("unsupported event type")
		}
		guildID := parseID(routing.GuildID)
		if guildID == 0 &&
			(event.Type == realtime.EventGuildCreated || event.Type == realtime.EventGuildUpdated || event.Type == realtime.EventGuildDeleted) {
			guildID = parseID(routing.ID)
		}
		if guildID <= 0 {
			return true, errors.New("guild event guild id is invalid")
		}
		if event.Type == realtime.EventGuildCreated && parseID(routing.OwnerID) <= 0 {
			return true, errors.New("guild created owner id is invalid")
		}
		if event.Type == realtime.EventGuildMemberJoined && parseID(routing.UserID) <= 0 {
			return true, errors.New("guild member joined user id is invalid")
		}
		return false, s.dispatchGuild(ctx, guildID, event, routing)
	}
}

func (s *Server) dispatchChannel(ctx context.Context, channelID int64, event eventEnvelope) error {
	nodes, err := s.resolver.Resolve(ctx, discovery.RouteChannel, channelID)
	if err != nil {
		return err
	}
	return s.forEachNode(ctx, nodes, func(ctx context.Context, client sessionv1.SessionServiceClient) error {
		req := new(sessionv1.DispatchChannelEventRequest)
		req.SetChannelId(channelID)
		req.SetEvent(protoEvent(event))
		_, err := client.DispatchChannelEvent(ctx, req)
		return err
	})
}

func (s *Server) dispatchGuild(ctx context.Context, guildID int64, event eventEnvelope, routing eventRouting) error {
	nodes, err := s.resolver.Resolve(ctx, discovery.RouteGuild, guildID)
	if err != nil {
		return err
	}
	if event.Type == realtime.EventGuildCreated || event.Type == realtime.EventGuildMemberJoined {
		userID := parseID(routing.OwnerID)
		if event.Type == realtime.EventGuildMemberJoined {
			userID = parseID(routing.UserID)
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

func parseID(value string) int64 {
	id, _ := strconv.ParseInt(value, 10, 64)
	return id
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
