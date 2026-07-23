package svc

import (
	"context"

	sn "github.com/bwmarrin/snowflake"
	"github.com/jmoiron/sqlx"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/zeromicro/go-zero/zrpc"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/kafka"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/user/v1/config"
	"github.com/soasurs/cordis/services/user/v1/internal/store"
)

// EventPublisher delivers relationship events to the user event stream.
type EventPublisher interface {
	Publish(ctx context.Context, key, payload []byte) error
}

type ServiceContext struct {
	Cfg       config.Config
	Store     store.Store
	Snowflake *sn.Node
	// Publisher is optional; events are skipped when nil.
	Publisher   EventPublisher
	MediaClient mediav1.MediaServiceClient
}

type Dependencies struct {
	Store       store.Store
	Snowflake   *sn.Node
	Kafka       *kgo.Client
	Publisher   EventPublisher
	MediaClient mediav1.MediaServiceClient
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
	mediaRPCClient, err := zrpc.NewClient(cfg.Services.Media)
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
		MediaClient: mediav1.NewMediaServiceClient(mediaRPCClient.Conn()),
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
		panic("user store is required")
	}
	if deps.Snowflake == nil {
		panic("snowflake node is required")
	}
	if deps.MediaClient == nil {
		panic("media client is required")
	}
	publisher := deps.Publisher
	if publisher == nil && deps.Kafka != nil {
		publisher = kafka.NewPublisher(deps.Kafka, cfg.Kafka.Topic)
	}
	return &ServiceContext{
		Cfg:         cfg,
		Store:       deps.Store,
		Snowflake:   deps.Snowflake,
		Publisher:   publisher,
		MediaClient: deps.MediaClient,
	}
}
