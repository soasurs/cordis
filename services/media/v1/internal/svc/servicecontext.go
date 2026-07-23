package svc

import (
	"fmt"

	sn "github.com/bwmarrin/snowflake"
	"github.com/jmoiron/sqlx"

	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/media/v1/config"
	"github.com/soasurs/cordis/services/media/v1/internal/objectstore"
	"github.com/soasurs/cordis/services/media/v1/internal/processing"
	"github.com/soasurs/cordis/services/media/v1/internal/store"
)

type ServiceContext struct {
	Cfg         config.Config
	Store       store.Store
	Snowflake   *sn.Node
	ObjectStore objectstore.ObjectStore
	Processor   *processing.Processor
}

type Dependencies struct {
	Store       store.Store
	Snowflake   *sn.Node
	ObjectStore objectstore.ObjectStore
	Processor   *processing.Processor
	DB          *sqlx.DB
}

func NewDependencies(cfg config.Config) (Dependencies, error) {
	node, err := snowflake.New()
	if err != nil {
		return Dependencies{}, fmt.Errorf("create snowflake node: %w", err)
	}

	db, err := database.NewPostgres(cfg.Database)
	if err != nil {
		return Dependencies{}, fmt.Errorf("create database: %w", err)
	}

	objStore, err := objectstore.NewS3(cfg.ObjectStore.ToObjectStoreConfig())
	if err != nil {
		db.Close()
		return Dependencies{}, fmt.Errorf("create object store: %w", err)
	}

	proc := processing.NewProcessor(objStore, cfg.Media)

	return Dependencies{
		Store:       store.New(db),
		Snowflake:   node,
		ObjectStore: objStore,
		Processor:   proc,
		DB:          db,
	}, nil
}

func NewServiceContextWithDependencies(cfg config.Config, deps Dependencies) *ServiceContext {
	if deps.Store == nil {
		panic("media store is required")
	}
	if deps.Snowflake == nil {
		panic("snowflake node is required")
	}
	if deps.ObjectStore == nil {
		panic("object store is required")
	}
	if deps.Processor == nil {
		panic("processor is required")
	}
	return &ServiceContext{
		Cfg:         cfg,
		Store:       deps.Store,
		Snowflake:   deps.Snowflake,
		ObjectStore: deps.ObjectStore,
		Processor:   deps.Processor,
	}
}
