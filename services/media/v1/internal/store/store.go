package store

import (
	"context"

	"github.com/jmoiron/sqlx"
)

type AssetStore interface {
	GetAsset(ctx context.Context, id int64) (*Asset, error)
	UpdateAsset(ctx context.Context, asset *Asset) error
}

type Store interface {
	AssetStore
	CreateAssetWithQuota(ctx context.Context, asset *Asset, activeUploadLimit int64) error
	ListAssets(ctx context.Context, ids []int64) ([]*Asset, error)
	ListExpiredUploads(ctx context.Context, before int64) ([]*Asset, error)
	AcquireAssetLock(ctx context.Context, id int64) (AssetStore, func(), error)
}

type SQLStore struct {
	db *sqlx.DB
	q  queryerExecer
}

func New(db *sqlx.DB) Store {
	return &SQLStore{db: db, q: db}
}

type queryerExecer interface {
	sqlx.QueryerContext
	sqlx.ExecerContext
}
