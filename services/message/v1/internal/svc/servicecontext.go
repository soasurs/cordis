package svc

import (
	"log/slog"
	"sync"

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

	// Relay is the background outbox poller. It recovers stale events
	// that were not flushed immediately after commit.
	Relay *outbox.Relay

	// ShutdownWg tracks in-flight flush goroutines and relay operations.
	// The main goroutine should Wait() on this during graceful shutdown
	// before closing Kafka and database connections.
	ShutdownWg *sync.WaitGroup
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

	svcCtx := &ServiceContext{
		Cfg:        cfg,
		Store:      deps.Store,
		Snowflake:  deps.Snowflake,
		Kafka:      deps.Kafka,
		ShutdownWg: &sync.WaitGroup{},
	}

	// Start the outbox relay if both Kafka and the outbox config are present.
	// The relay handles stale event recovery — events that were written to
	// the outbox but never flushed (e.g. due to a crash).
	if deps.Kafka != nil && deps.DB != nil {
		relayCfg := cfg.Outbox.RelayConfig()
		producer := &outbox.FranzProducer{Client: deps.Kafka}
		svcCtx.Relay = outbox.NewRelay(relayCfg, deps.DB, producer, slog.Default())
	}

	return svcCtx
}
