package svc

import (
	"errors"

	sn "github.com/bwmarrin/snowflake"
	"github.com/jmoiron/sqlx"
	"github.com/zeromicro/go-zero/zrpc"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/authenticator/v1/config"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/store"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/token"
)

type ServiceContext struct {
	Cfg        config.Config
	Store      store.Store
	Tokens     *token.Manager
	Snowflake  *sn.Node
	UserClient userv1.UserServiceClient
}

type Dependencies struct {
	Store      store.Store
	Tokens     *token.Manager
	Snowflake  *sn.Node
	UserClient userv1.UserServiceClient
	DB         *sqlx.DB
}

func NewDependencies(cfg config.Config) (Dependencies, error) {
	if cfg.Sessions.TTL <= 0 {
		return Dependencies{}, errors.New("session ttl must be positive")
	}

	node, err := snowflake.New()
	if err != nil {
		return Dependencies{}, err
	}

	tokenManager, err := token.NewManager(token.Config{
		Issuer:        cfg.Tokens.Issuer,
		AccessSecret:  cfg.Tokens.Access.Secret,
		RefreshSecret: cfg.Tokens.Refresh.Secret,
		AccessTTL:     cfg.Tokens.Access.TTL,
		RefreshTTL:    cfg.Tokens.Refresh.TTL,
	})
	if err != nil {
		return Dependencies{}, err
	}

	userRPCClient, err := zrpc.NewClient(cfg.Services.User)
	if err != nil {
		return Dependencies{}, err
	}

	db, err := database.NewPostgres(cfg.Database)
	if err != nil {
		return Dependencies{}, err
	}

	return Dependencies{
		Store:      store.New(db),
		Tokens:     tokenManager,
		Snowflake:  node,
		UserClient: userv1.NewUserServiceClient(userRPCClient.Conn()),
		DB:         db,
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
	if cfg.Sessions.TTL <= 0 {
		panic("session ttl must be positive")
	}
	if deps.Store == nil {
		panic("authenticator store is required")
	}
	if deps.Tokens == nil {
		panic("token manager is required")
	}
	if deps.Snowflake == nil {
		panic("snowflake node is required")
	}
	if deps.UserClient == nil {
		panic("user client is required")
	}
	return &ServiceContext{
		Cfg:        cfg,
		Store:      deps.Store,
		Tokens:     deps.Tokens,
		Snowflake:  deps.Snowflake,
		UserClient: deps.UserClient,
	}
}
