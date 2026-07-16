package svc

import (
	"context"

	sn "github.com/bwmarrin/snowflake"
	"github.com/jmoiron/sqlx"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/zeromicro/go-zero/zrpc"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/kafka"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/message/v1/config"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
)

// EventPublisher publishes a serialized message event.
type EventPublisher interface {
	Publish(ctx context.Context, key, payload []byte) error
}

type ServiceContext struct {
	Cfg         config.Config
	Store       store.Store
	Snowflake   *sn.Node
	Publisher   EventPublisher
	GuildClient guildv1.GuildServiceClient
}

type Dependencies struct {
	Store       store.Store
	Snowflake   *sn.Node
	Kafka       *kgo.Client
	Publisher   EventPublisher
	GuildClient guildv1.GuildServiceClient
	DB          *sqlx.DB
}

func NewDependencies(cfg config.Config) (Dependencies, error) {
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

	var kafkaClient *kgo.Client
	if len(cfg.Kafka.Seeds) > 0 {
		kafkaClient, err = kafka.NewProducer(cfg.Kafka.ProducerConfig())
		if err != nil {
			db.Close()
			return Dependencies{}, err
		}
	}

	return Dependencies{
		Store:       store.New(db),
		Snowflake:   node,
		Kafka:       kafkaClient,
		DB:          db,
		GuildClient: guildv1.NewGuildServiceClient(guildRPCClient.Conn()),
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

	publisher := deps.Publisher
	if publisher == nil && deps.Kafka != nil {
		publisher = &kafkaPublisher{
			client: deps.Kafka,
			topic:  cfg.Kafka.Topic,
		}
	}
	return &ServiceContext{
		Cfg:         cfg,
		Store:       deps.Store,
		Snowflake:   deps.Snowflake,
		Publisher:   publisher,
		GuildClient: deps.GuildClient,
	}
}

type kafkaPublisher struct {
	client *kgo.Client
	topic  string
}

func (p *kafkaPublisher) Publish(ctx context.Context, key, payload []byte) error {
	return p.client.ProduceSync(ctx, &kgo.Record{
		Topic: p.topic,
		Key:   key,
		Value: payload,
	}).FirstErr()
}
