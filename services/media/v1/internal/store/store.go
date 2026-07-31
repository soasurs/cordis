package store

import (
	"context"
	"database/sql"

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
	Transact(ctx context.Context, fn func(txStore Store) error) error
	ClaimMediaIdempotency(ctx context.Context, params ClaimMediaIdempotencyParams) (*MediaIdempotencyClaim, error)
}

type SQLStore struct {
	db *sqlx.DB
	q  sqlx.ExtContext
}

func New(db *sqlx.DB) Store {
	return &SQLStore{
		db: db,
		q:  db,
	}
}

type queryerExecer interface {
	sqlx.QueryerContext
	sqlx.ExecerContext
}

// assetLockStore exposes read and update operations on a dedicated connection
// holding an asset advisory lock. It intentionally lacks create and
// idempotency methods, which require an ExtContext.
type assetLockStore struct {
	q queryerExecer
}

func (s *assetLockStore) GetAsset(ctx context.Context, id int64) (*Asset, error) {
	return getAsset(ctx, s.q, id)
}

func (s *assetLockStore) UpdateAsset(ctx context.Context, asset *Asset) error {
	return updateAsset(ctx, s.q, asset)
}

// Transact runs fn inside a database transaction. Store methods called with
// the returned store reuse the transaction; a returned error rolls back and a
// panic rolls back before re-panicking.
func (s *SQLStore) Transact(ctx context.Context, fn func(txStore Store) error) (err error) {
	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	err = fn(&SQLStore{db: s.db, q: tx})
	if err != nil {
		return err
	}
	return tx.Commit()
}
