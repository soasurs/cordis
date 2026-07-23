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
	Cfg                   config.Config
	Store                 store.Store
	Snowflake             *sn.Node
	PublicObjectStore     objectstore.ObjectStore
	StagingObjectStore    objectstore.ObjectStore
	AttachmentObjectStore objectstore.ObjectStore
	Processor             *processing.Processor
}

type Dependencies struct {
	Store                 store.Store
	Snowflake             *sn.Node
	PublicObjectStore     objectstore.ObjectStore
	StagingObjectStore    objectstore.ObjectStore
	AttachmentObjectStore objectstore.ObjectStore
	Processor             *processing.Processor
	DB                    *sqlx.DB
}

func NewDependencies(cfg config.Config) (Dependencies, error) {
	if err := cfg.Validate(); err != nil {
		return Dependencies{}, fmt.Errorf("validate config: %w", err)
	}
	node, err := snowflake.New()
	if err != nil {
		return Dependencies{}, fmt.Errorf("create snowflake node: %w", err)
	}

	db, err := database.NewPostgres(cfg.Database)
	if err != nil {
		return Dependencies{}, fmt.Errorf("create database: %w", err)
	}

	publicStore, err := objectstore.NewS3(cfg.ObjectStore.ToObjectStoreConfig(cfg.ObjectStore.PublicBucket))
	if err != nil {
		db.Close()
		return Dependencies{}, fmt.Errorf("create public object store: %w", err)
	}
	stagingStore, err := objectstore.NewS3(cfg.ObjectStore.ToObjectStoreConfig(cfg.ObjectStore.StagingBucket))
	if err != nil {
		db.Close()
		return Dependencies{}, fmt.Errorf("create staging object store: %w", err)
	}
	attachmentStore, err := objectstore.NewS3(cfg.ObjectStore.ToObjectStoreConfig(cfg.ObjectStore.AttachmentBucket))
	if err != nil {
		db.Close()
		return Dependencies{}, fmt.Errorf("create attachment object store: %w", err)
	}

	proc := processing.NewProcessor(stagingStore, publicStore, cfg.Media)

	return Dependencies{
		Store:                 store.New(db),
		Snowflake:             node,
		PublicObjectStore:     publicStore,
		StagingObjectStore:    stagingStore,
		AttachmentObjectStore: attachmentStore,
		Processor:             proc,
		DB:                    db,
	}, nil
}

func NewServiceContextWithDependencies(cfg config.Config, deps Dependencies) *ServiceContext {
	if deps.Store == nil {
		panic("media store is required")
	}
	if deps.Snowflake == nil {
		panic("snowflake node is required")
	}
	if deps.PublicObjectStore == nil {
		panic("public object store is required")
	}
	if deps.StagingObjectStore == nil {
		panic("staging object store is required")
	}
	if deps.AttachmentObjectStore == nil {
		panic("attachment object store is required")
	}
	if deps.Processor == nil {
		panic("processor is required")
	}
	return &ServiceContext{
		Cfg:                   cfg,
		Store:                 deps.Store,
		Snowflake:             deps.Snowflake,
		PublicObjectStore:     deps.PublicObjectStore,
		StagingObjectStore:    deps.StagingObjectStore,
		AttachmentObjectStore: deps.AttachmentObjectStore,
		Processor:             deps.Processor,
	}
}
