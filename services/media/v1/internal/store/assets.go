package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

func (s *SQLStore) CreateAssetWithQuota(
	ctx context.Context,
	asset *Asset,
	activeUploadLimit int64,
) (err error) {
	if activeUploadLimit <= 0 {
		return errors.New("active upload limit must be positive")
	}
	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin create asset transaction: %w", err)
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

	lockKey := fmt.Sprintf("cordis:media:upload-quota:%d", asset.CreatedByUserID)
	if _, err = tx.ExecContext(ctx, lockUploadQuotaScopeStatement, lockKey); err != nil {
		return fmt.Errorf("lock upload quota: %w", err)
	}
	var count int64
	if err = tx.GetContext(
		ctx,
		&count,
		countActiveUploadsQuery,
		asset.CreatedByUserID,
	); err != nil {
		return fmt.Errorf("count user active uploads: %w", err)
	}
	if count >= activeUploadLimit {
		return ErrActiveUploadLimit
	}
	if _, err = tx.NamedExecContext(ctx, createAssetStatement, asset); err != nil {
		return fmt.Errorf("create asset: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit create asset: %w", err)
	}
	return nil
}

func (s *SQLStore) GetAsset(ctx context.Context, id int64) (*Asset, error) {
	return getAsset(ctx, s.q, id)
}

func (s *SQLStore) ListAssets(ctx context.Context, ids []int64) ([]*Asset, error) {
	if len(ids) == 0 {
		return []*Asset{}, nil
	}
	var assets []*Asset
	if err := sqlx.SelectContext(ctx, s.q, &assets, listAssetsQuery, pq.Array(ids)); err != nil {
		return nil, fmt.Errorf("list assets: %w", err)
	}
	return assets, nil
}

func getAsset(ctx context.Context, q sqlx.QueryerContext, id int64) (*Asset, error) {
	var a Asset
	if err := sqlx.GetContext(ctx, q, &a, getAssetQuery, id); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get asset: %w", err)
	}
	return &a, nil
}

func (s *SQLStore) UpdateAsset(ctx context.Context, asset *Asset) error {
	return updateAsset(ctx, s.q, asset)
}

func updateAsset(ctx context.Context, q sqlx.ExecerContext, asset *Asset) error {
	asset.UpdatedAt = time.Now().UnixMilli()
	query, args, err := sqlx.Named(updateAssetStatement, asset)
	if err != nil {
		return fmt.Errorf("bind update asset: %w", err)
	}
	result, err := q.ExecContext(ctx, sqlx.Rebind(sqlx.DOLLAR, query), args...)
	if err != nil {
		return fmt.Errorf("update asset: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read update asset result: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLStore) ListExpiredUploads(ctx context.Context, before int64) ([]*Asset, error) {
	var assets []*Asset
	if err := sqlx.SelectContext(ctx, s.q, &assets, listExpiredUploadsQuery, before); err != nil {
		return nil, fmt.Errorf("list expired uploads: %w", err)
	}
	return assets, nil
}

func (s *SQLStore) AcquireAssetLock(ctx context.Context, id int64) (AssetStore, func(), error) {
	conn, err := s.db.Connx(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire database connection: %w", err)
	}
	lockKey := fmt.Sprintf("cordis:media:asset:%d", id)
	if _, err := conn.ExecContext(ctx, lockAssetStatement, lockKey); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("lock asset: %w", err)
	}

	var once sync.Once
	unlock := func() {
		once.Do(func() {
			unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = conn.ExecContext(unlockCtx, unlockAssetStatement, lockKey)
			_ = conn.Close()
		})
	}
	return &SQLStore{db: s.db, q: conn}, unlock, nil
}
