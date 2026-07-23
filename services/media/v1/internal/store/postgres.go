package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

type postgresStore struct {
	db *sqlx.DB
}

type lockedAssetStore struct {
	q interface {
		sqlx.QueryerContext
		sqlx.ExecerContext
	}
}

const (
	insertAssetQuery = `INSERT INTO assets (
		id, user_id, kind, status, storage_backend, staging_key, published_key,
		expected_size, content_type, expires_at, created_at, updated_at
	) VALUES (
		:id, :user_id, :kind, :status, :storage_backend, :staging_key, :published_key,
		:expected_size, :content_type, :expires_at, :created_at, :updated_at
	)`
	lockQuotaScopeQuery = `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`
	lockAssetQuery      = `SELECT pg_advisory_lock(hashtextextended($1, 0))`
	unlockAssetQuery    = `SELECT pg_advisory_unlock(hashtextextended($1, 0))`
)

func NewPostgres(db *sqlx.DB) Store {
	return &postgresStore{db: db}
}

func (s *postgresStore) CreateAssetWithQuota(
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

	lockKey := fmt.Sprintf("cordis:media:upload-quota:%d", asset.UserID)
	if _, err = tx.ExecContext(ctx, lockQuotaScopeQuery, lockKey); err != nil {
		return fmt.Errorf("lock upload quota: %w", err)
	}
	var count int64
	if err = tx.GetContext(ctx, &count, `SELECT COUNT(*) FROM assets
		WHERE user_id = $1
		AND status = 'CREATED'
		AND deleted_at = 0`, asset.UserID); err != nil {
		return fmt.Errorf("count user active uploads: %w", err)
	}
	if count >= activeUploadLimit {
		return ErrActiveUploadLimit
	}
	if _, err = tx.NamedExecContext(ctx, insertAssetQuery, asset); err != nil {
		return fmt.Errorf("create asset: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit create asset: %w", err)
	}
	return nil
}

func (s *postgresStore) GetAsset(ctx context.Context, id int64) (*Asset, error) {
	return getAsset(ctx, s.db, id)
}

func (s *lockedAssetStore) GetAsset(ctx context.Context, id int64) (*Asset, error) {
	return getAsset(ctx, s.q, id)
}

func getAsset(ctx context.Context, q sqlx.QueryerContext, id int64) (*Asset, error) {
	query := `SELECT id, user_id, kind, status, storage_backend, staging_key,
			published_key, expected_size, actual_size, content_type,
			expires_at, width, height, variants, error_message,
			created_at, updated_at, deleted_at
		FROM assets WHERE id = $1 AND deleted_at = 0`
	var a Asset
	if err := sqlx.GetContext(ctx, q, &a, query, id); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get asset: %w", err)
	}
	return &a, nil
}

func (s *postgresStore) UpdateAsset(ctx context.Context, asset *Asset) error {
	return updateAsset(ctx, s.db, asset)
}

func (s *lockedAssetStore) UpdateAsset(ctx context.Context, asset *Asset) error {
	return updateAsset(ctx, s.q, asset)
}

func updateAsset(ctx context.Context, q sqlx.ExecerContext, asset *Asset) error {
	asset.UpdatedAt = time.Now().UnixMilli()
	query := `UPDATE assets SET
		status = :status,
		storage_backend = :storage_backend,
		published_key = :published_key,
		actual_size = :actual_size,
		content_type = :content_type,
		width = :width,
		height = :height,
		variants = :variants,
		error_message = :error_message,
		updated_at = :updated_at
		WHERE id = :id AND deleted_at = 0`
	query, args, err := sqlx.Named(query, asset)
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

func (s *postgresStore) ListExpiredUploads(ctx context.Context, before int64) ([]*Asset, error) {
	query := `SELECT id, user_id, kind, status, storage_backend, staging_key,
			published_key, expected_size, actual_size, content_type,
			expires_at, width, height, variants, error_message,
			created_at, updated_at, deleted_at
		FROM assets
		WHERE status = 'CREATED'
		AND expires_at > 0 AND expires_at < $1
		AND deleted_at = 0
		LIMIT 100`
	var assets []*Asset
	if err := s.db.SelectContext(ctx, &assets, query, before); err != nil {
		return nil, fmt.Errorf("list expired uploads: %w", err)
	}
	return assets, nil
}

func (s *postgresStore) AcquireAssetLock(ctx context.Context, id int64) (AssetStore, func(), error) {
	conn, err := s.db.Connx(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire database connection: %w", err)
	}
	lockKey := fmt.Sprintf("cordis:media:asset:%d", id)
	if _, err := conn.ExecContext(ctx, lockAssetQuery, lockKey); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("lock asset: %w", err)
	}

	var once sync.Once
	unlock := func() {
		once.Do(func() {
			unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = conn.ExecContext(unlockCtx, unlockAssetQuery, lockKey)
			_ = conn.Close()
		})
	}
	return &lockedAssetStore{q: conn}, unlock, nil
}
