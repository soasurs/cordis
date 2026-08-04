package svc

import (
	"context"

	sn "github.com/bwmarrin/snowflake"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zeromicro/go-zero/zrpc"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	"github.com/soasurs/cordis/pkg/cursor"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/user/v1/config"
	"github.com/soasurs/cordis/services/user/v1/internal/store"
)

type ServiceContext struct {
	Cfg         config.Config
	Store       store.Store
	Snowflake   *sn.Node
	Cursors     *cursor.Codec
	MediaClient mediav1.MediaServiceClient
}

type Dependencies struct {
	Store       store.Store
	Snowflake   *sn.Node
	Cursors     *cursor.Codec
	MediaClient mediav1.MediaServiceClient
	DB          *pgxpool.Pool
}

func NewDependencies(cfg config.Config) (Dependencies, error) {
	node, err := snowflake.New()
	if err != nil {
		return Dependencies{}, err
	}
	cursors, err := cursor.NewCodec(cfg.Cursor.Secret)
	if err != nil {
		return Dependencies{}, err
	}

	db, err := database.NewPostgresPool(context.Background(), cfg.Database)
	if err != nil {
		return Dependencies{}, err
	}
	mediaRPCClient, err := zrpc.NewClient(cfg.Services.Media)
	if err != nil {
		db.Close()
		return Dependencies{}, err
	}

	return Dependencies{
		Store:       store.New(db),
		Snowflake:   node,
		Cursors:     cursors,
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
	if deps.Cursors == nil {
		panic("cursor codec is required")
	}
	if deps.MediaClient == nil {
		panic("media client is required")
	}
	return &ServiceContext{
		Cfg:         cfg,
		Store:       deps.Store,
		Snowflake:   deps.Snowflake,
		Cursors:     deps.Cursors,
		MediaClient: deps.MediaClient,
	}
}
