package svc

import (
	"context"
	"fmt"

	sn "github.com/bwmarrin/snowflake"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zeromicro/go-zero/zrpc"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/concurrencylimit"
	"github.com/soasurs/cordis/pkg/cursor"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/message/v1/config"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
)

// ConcurrencyLimiter acquires weighted process-local capacity.
type ConcurrencyLimiter interface {
	Acquire(ctx context.Context, weight int64) (func(), error)
}

type ServiceContext struct {
	Cfg               config.Config
	Store             store.Store
	Snowflake         *sn.Node
	Cursors           *cursor.Codec
	GuildClient       guildv1.GuildServiceClient
	UserClient        userv1.UserServiceClient
	MediaClient       mediav1.MediaServiceClient
	ReadStatesLimiter ConcurrencyLimiter
}

type Dependencies struct {
	Store             store.Store
	Snowflake         *sn.Node
	Cursors           *cursor.Codec
	GuildClient       guildv1.GuildServiceClient
	UserClient        userv1.UserServiceClient
	MediaClient       mediav1.MediaServiceClient
	ReadStatesLimiter ConcurrencyLimiter
	DB                *pgxpool.Pool
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
	cursors, err := cursor.NewCodec(cfg.Cursor.Secret)
	if err != nil {
		return Dependencies{}, err
	}

	db, err := database.NewPostgresPool(context.Background(), cfg.Database)
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
	mediaRPCClient, err := zrpc.NewClient(cfg.Services.Media)
	if err != nil {
		db.Close()
		return Dependencies{}, err
	}

	return Dependencies{
		Store:             store.New(db),
		Snowflake:         node,
		Cursors:           cursors,
		DB:                db,
		GuildClient:       guildv1.NewGuildServiceClient(guildRPCClient.Conn()),
		UserClient:        userv1.NewUserServiceClient(userRPCClient.Conn()),
		MediaClient:       mediav1.NewMediaServiceClient(mediaRPCClient.Conn()),
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
	if deps.Cursors == nil {
		panic("cursor codec is required")
	}
	if deps.GuildClient == nil {
		panic("guild client is required")
	}
	if deps.UserClient == nil {
		panic("user client is required")
	}
	if deps.MediaClient == nil {
		panic("media client is required")
	}
	if cfg.ReadStates.MaxConcurrentChannels > 0 && deps.ReadStatesLimiter == nil {
		panic("read states concurrency limiter is required")
	}
	return &ServiceContext{
		Cfg:               cfg,
		Store:             deps.Store,
		Snowflake:         deps.Snowflake,
		Cursors:           deps.Cursors,
		GuildClient:       deps.GuildClient,
		UserClient:        deps.UserClient,
		MediaClient:       deps.MediaClient,
		ReadStatesLimiter: deps.ReadStatesLimiter,
	}
}
