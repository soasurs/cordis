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

func NewServiceContext(cfg config.Config) *ServiceContext {
	node, err := snowflake.New()
	if err != nil {
		panic(err)
	}

	db, err := database.NewPostgres(cfg.Database)
	if err != nil {
		panic(err)
	}

	return &ServiceContext{
		Cfg:       cfg,
		Store:     store.New(db),
		Snowflake: node,
	}
}
