package svc

import (
	sn "github.com/bwmarrin/snowflake"
	"github.com/jmoiron/sqlx"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/kafka"
	"github.com/soasurs/cordis/pkg/outbox"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/message/v1/config"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
)

type ServiceContext struct {
	Cfg       config.Config
	Store     store.Store
	Snowflake *sn.Node

	// Kafka is the franz-go client for publishing events.
	// Nil if Kafka is not configured.
	Kafka *kgo.Client

	// Relay is the background outbox dispatcher. It picks up pending
	// events and publishes them to Kafka. Handlers wake it via Notify()
	// after committing a transaction that inserts outbox events.
	Relay *outbox.Relay

	// Cached outbox config values to avoid repeated allocations in handlers.
	OutboxMaxRetries     int
	OutboxPartitionCount int
}

type Dependencies struct {
	Store     store.Store
	Snowflake *sn.Node
	Kafka     *kgo.Client
	DB        *sqlx.DB
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

	var kafkaClient *kgo.Client
	if len(cfg.Kafka.Seeds) > 0 {
		kafkaClient, err = kafka.NewProducer(cfg.Kafka.ProducerConfig())
		if err != nil {
			db.Close()
			return Dependencies{}, err
		}
	}

	return Dependencies{
		Store:     store.New(db),
		Snowflake: node,
		Kafka:     kafkaClient,
		DB:        db,
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

	relayCfg := cfg.Outbox.RelayConfig()
	svcCtx := &ServiceContext{
		Cfg:                  cfg,
		Store:                deps.Store,
		Snowflake:            deps.Snowflake,
		Kafka:                deps.Kafka,
		OutboxMaxRetries:     relayCfg.MaxRetries,
		OutboxPartitionCount: relayCfg.PartitionCount,
	}

	if deps.Kafka != nil && deps.DB != nil {
		producer := &outbox.FranzProducer{Client: deps.Kafka}
		svcCtx.Relay = outbox.NewRelay(relayCfg, deps.DB, producer)
	}

	return svcCtx
}
