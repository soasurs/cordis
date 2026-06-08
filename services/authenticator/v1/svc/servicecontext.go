package svc

import (
	sn "github.com/bwmarrin/snowflake"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/authenticator/v1/config"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/store"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/token"
	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Cfg        config.Config
	Store      store.Store
	Tokens     *token.Manager
	Snowflake  *sn.Node
	UserClient userv1.UserServiceClient
}

func NewServiceContext(cfg config.Config) *ServiceContext {
	if cfg.Sessions.TTL <= 0 {
		panic("session ttl must be positive")
	}

	db, err := database.NewPostgres(cfg.Database)
	if err != nil {
		panic(err)
	}

	node, err := snowflake.New()
	if err != nil {
		panic(err)
	}

	tokenManager, err := token.NewManager(token.Config{
		Issuer:        cfg.Tokens.Issuer,
		AccessSecret:  cfg.Tokens.Access.Secret,
		RefreshSecret: cfg.Tokens.Refresh.Secret,
		AccessTTL:     cfg.Tokens.Access.TTL,
		RefreshTTL:    cfg.Tokens.Refresh.TTL,
	})
	if err != nil {
		panic(err)
	}

	userClient := userv1.NewUserServiceClient(zrpc.MustNewClient(cfg.Services.User).Conn())

	return &ServiceContext{
		Cfg:        cfg,
		Store:      store.New(db),
		Tokens:     tokenManager,
		Snowflake:  node,
		UserClient: userClient,
	}
}
