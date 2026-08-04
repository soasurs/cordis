package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
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
	db *pgxpool.Pool
	q  queryer
}

// queryer is the pgx-native query surface used by SQLStore. The top-level
// store uses the pool; transactions replace it with the pgx transaction.
type queryer interface {
	Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, query string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) pgx.Row
}

func New(db *pgxpool.Pool) Store {
	return &SQLStore{
		db: db,
		q:  db,
	}
}

// assetLockStore exposes read and update operations on a dedicated connection
// holding an asset advisory lock. It intentionally lacks create and
// idempotency methods, which require an ExtContext.
type assetLockStore struct {
	q queryer
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
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	err = fn(&SQLStore{db: s.db, q: tx})
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
