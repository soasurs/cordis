package store

import "context"

type AssetStore interface {
	GetAsset(ctx context.Context, id int64) (*Asset, error)
	UpdateAsset(ctx context.Context, asset *Asset) error
}

type Store interface {
	AssetStore
	CreateAssetWithQuota(ctx context.Context, asset *Asset, activeUploadLimit int64) error
	ListExpiredUploads(ctx context.Context, before int64) ([]*Asset, error)
	AcquireAssetLock(ctx context.Context, id int64) (AssetStore, func(), error)
}
