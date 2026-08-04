package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *SQLStore) CreateAssetWithQuota(
	ctx context.Context,
	asset *Asset,
	activeUploadLimit int64,
) error {
	if activeUploadLimit <= 0 {
		return errors.New("active upload limit must be positive")
	}
	lockKey := fmt.Sprintf("cordis:media:upload-quota:%d", asset.CreatedByUserID)
	if _, err := s.q.Exec(ctx, lockUploadQuotaScopeStatement, lockKey); err != nil {
		return fmt.Errorf("lock upload quota: %w", err)
	}
	count, err := scanOne(ctx, s.q, countActiveUploadsQuery, pgx.RowTo[int64], asset.CreatedByUserID)
	if err != nil {
		return fmt.Errorf("count user active uploads: %w", err)
	}
	if count >= activeUploadLimit {
		return ErrActiveUploadLimit
	}
	if _, err := s.q.Exec(
		ctx,
		createAssetStatement,
		asset.ID,
		asset.CreatedByUserID,
		asset.SubjectID,
		asset.Kind,
		asset.Status,
		asset.StorageBackend,
		asset.StagingKey,
		asset.PublishedKey,
		asset.Filename,
		asset.StorageToken,
		asset.ExpectedSize,
		asset.ContentType,
		asset.ExpiresAt,
		asset.CreatedAt,
		asset.UpdatedAt,
	); err != nil {
		return fmt.Errorf("create asset: %w", err)
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
	rows, err := scanMany(ctx, s.q, listAssetsQuery, pgx.RowToStructByName[Asset], ids)
	if err != nil {
		return nil, fmt.Errorf("list assets: %w", err)
	}
	assets := make([]*Asset, 0, len(rows))
	for i := range rows {
		assets = append(assets, &rows[i])
	}
	return assets, nil
}

func getAsset(ctx context.Context, q queryer, id int64) (*Asset, error) {
	a, err := scanOne(ctx, q, getAssetQuery, pgx.RowToStructByName[Asset], id)
	if err != nil {
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

func updateAsset(ctx context.Context, q queryer, asset *Asset) error {
	asset.UpdatedAt = time.Now().UnixMilli()
	tag, err := q.Exec(
		ctx,
		updateAssetStatement,
		asset.Status,
		asset.StorageBackend,
		asset.PublishedKey,
		asset.ActualSize,
		asset.ContentType,
		asset.Width,
		asset.Height,
		asset.Blurhash,
		asset.ErrorMessage,
		asset.UpdatedAt,
		asset.ID,
	)
	if err != nil {
		return fmt.Errorf("update asset: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLStore) ListExpiredUploads(ctx context.Context, before int64) ([]*Asset, error) {
	rows, err := scanMany(ctx, s.q, listExpiredUploadsQuery, pgx.RowToStructByName[Asset], before)
	if err != nil {
		return nil, fmt.Errorf("list expired uploads: %w", err)
	}
	assets := make([]*Asset, 0, len(rows))
	for i := range rows {
		assets = append(assets, &rows[i])
	}
	return assets, nil
}

func (s *SQLStore) AcquireAssetLock(ctx context.Context, id int64) (AssetStore, func(), error) {
	conn, err := s.db.Acquire(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire database connection: %w", err)
	}
	lockKey := fmt.Sprintf("cordis:media:asset:%d", id)
	if _, err := conn.Exec(ctx, lockAssetStatement, lockKey); err != nil {
		conn.Release()
		return nil, nil, fmt.Errorf("lock asset: %w", err)
	}

	var once sync.Once
	unlock := func() {
		once.Do(func() {
			unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = conn.Exec(unlockCtx, unlockAssetStatement, lockKey)
			conn.Release()
		})
	}
	return &assetLockStore{q: conn}, unlock, nil
}
