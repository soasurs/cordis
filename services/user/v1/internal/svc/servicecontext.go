package svc

import (
	sn "github.com/bwmarrin/snowflake"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/snowflake"

	"github.com/soasurs/cordis/services/user/v1/config"
	"github.com/soasurs/cordis/services/user/v1/internal/store"
)

type ServiceContext struct {
	Cfg       config.Config
	Store     store.Store
	Snowflake *sn.Node
}

type Dependencies struct {
	Store     store.Store
	Snowflake *sn.Node
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

	return Dependencies{
		Store:     store.New(db),
		Snowflake: node,
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
	return &ServiceContext{
		Cfg:       cfg,
		Store:     deps.Store,
		Snowflake: deps.Snowflake,
	}
}
