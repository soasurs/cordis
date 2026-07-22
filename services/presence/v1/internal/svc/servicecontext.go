package svc

import (
	"context"

	sn "github.com/bwmarrin/snowflake"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/soasurs/cordis/pkg/kafka"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/presence/v1/config"
	"github.com/soasurs/cordis/services/presence/v1/internal/store"
)

// EventPublisher delivers presence transition events.
type EventPublisher interface {
	Publish(ctx context.Context, key, payload []byte) error
}

type ServiceContext struct {
	Cfg       config.Config
	Store     store.Store
	Snowflake *sn.Node
	// Publisher is optional; transition events are skipped when nil.
	Publisher EventPublisher
}

type Dependencies struct {
	Store     store.Store
	Snowflake *sn.Node
	Redis     *redis.Redis
	Kafka     *kgo.Client
	Publisher EventPublisher
}

func NewDependencies(cfg config.Config) (Dependencies, error) {
	node, err := snowflake.New()
	if err != nil {
		return Dependencies{}, err
	}

	rds, err := redis.NewRedis(cfg.Redis)
	if err != nil {
		return Dependencies{}, err
	}

	var kafkaClient *kgo.Client
	if len(cfg.Kafka.Seeds) > 0 {
		kafkaClient, err = kafka.NewProducer(cfg.Kafka.ProducerConfig())
		if err != nil {
			return Dependencies{}, err
		}
	}

	return Dependencies{
		Store:     store.NewRedisStore(rds, cfg.Presence.UserSessionTTL(), cfg.Kafka.PublishTimeout()),
		Snowflake: node,
		Redis:     rds,
		Kafka:     kafkaClient,
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
		panic("presence store is required")
	}
	if deps.Snowflake == nil {
		panic("snowflake node is required")
	}
	publisher := deps.Publisher
	if publisher == nil && deps.Kafka != nil {
		publisher = kafka.NewPublisher(deps.Kafka, cfg.Kafka.Topic)
	}
	return &ServiceContext{
		Cfg:       cfg,
		Store:     deps.Store,
		Snowflake: deps.Snowflake,
		Publisher: publisher,
	}
}
