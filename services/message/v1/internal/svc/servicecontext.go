package svc

import (
	"context"
	"fmt"

	sn "github.com/bwmarrin/snowflake"
	"github.com/jmoiron/sqlx"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/zeromicro/go-zero/zrpc"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/concurrencylimit"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/kafka"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/message/v1/config"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
)

// ConcurrencyLimiter acquires weighted process-local capacity.
type ConcurrencyLimiter interface {
	Acquire(ctx context.Context, weight int64) (func(), error)
}

// EventPublisher publishes a serialized message event.
type EventPublisher interface {
	Publish(ctx context.Context, key, payload []byte) error
}

type ServiceContext struct {
	Cfg               config.Config
	Store             store.Store
	Snowflake         *sn.Node
	Publisher         EventPublisher
	GuildClient       guildv1.GuildServiceClient
	UserClient        userv1.UserServiceClient
	ReadStatesLimiter ConcurrencyLimiter
}

type Dependencies struct {
	Store             store.Store
	Snowflake         *sn.Node
	Kafka             *kgo.Client
	Publisher         EventPublisher
	GuildClient       guildv1.GuildServiceClient
	UserClient        userv1.UserServiceClient
	ReadStatesLimiter ConcurrencyLimiter
	DB                *sqlx.DB
}

func NewDependencies(cfg config.Config) (Dependencies, error) {
	readStatesLimiter, err := concurrencylimit.New("message_read_states", cfg.ReadStates.MaxConcurrentChannels)
	if err != nil {
		return Dependencies{}, fmt.Errorf("create read states concurrency limiter: %w", err)
	}
	node, err := snowflake.New()
	if err != nil {
		return Dependencies{}, err
	}

	db, err := database.NewPostgres(cfg.Database)
	if err != nil {
		return Dependencies{}, err
	}
	guildRPCClient, err := zrpc.NewClient(cfg.Services.Guild)
	if err != nil {
		db.Close()
		return Dependencies{}, err
	}
	userRPCClient, err := zrpc.NewClient(cfg.Services.User)
	if err != nil {
		db.Close()
		return Dependencies{}, err
	}

	var kafkaClient *kgo.Client
	if len(cfg.Kafka.Seeds) > 0 {
		kafkaClient, err = kafka.NewProducer(cfg.Kafka.ProducerConfig())
		if err != nil {
			db.Close()
			return Dependencies{}, err
		}
	}

	return Dependencies{
		Store:             store.New(db),
		Snowflake:         node,
		Kafka:             kafkaClient,
		DB:                db,
		GuildClient:       guildv1.NewGuildServiceClient(guildRPCClient.Conn()),
		UserClient:        userv1.NewUserServiceClient(userRPCClient.Conn()),
		ReadStatesLimiter: readStatesLimiter,
	}, nil
}

func NewServiceContext(cfg config.Config) *ServiceContext {
	deps, err := NewDependencies(cfg)
	if err != nil {
		panic(err)
	}
	return NewServiceContextWithDependencies(cfg, deps)
}

func NewServiceContextWithDependencies(cfg config.Config, deps Dependencies) *ServiceContext {
	if deps.Store == nil {
		panic("message store is required")
	}
	if deps.Snowflake == nil {
		panic("snowflake node is required")
	}
	if deps.GuildClient == nil {
		panic("guild client is required")
	}
	if deps.UserClient == nil {
		panic("user client is required")
	}
	if cfg.ReadStates.MaxConcurrentChannels > 0 && deps.ReadStatesLimiter == nil {
		panic("read states concurrency limiter is required")
	}
	publisher := deps.Publisher
	if publisher == nil && deps.Kafka != nil {
		publisher = kafka.NewPublisher(deps.Kafka, cfg.Kafka.Topic)
	}
	return &ServiceContext{
		Cfg:               cfg,
		Store:             deps.Store,
		Snowflake:         deps.Snowflake,
		Publisher:         publisher,
		GuildClient:       deps.GuildClient,
		UserClient:        deps.UserClient,
		ReadStatesLimiter: deps.ReadStatesLimiter,
	}
}
