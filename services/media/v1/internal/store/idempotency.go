package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

type mediaIdempotencyRow struct {
	AssetID     int64  `db:"asset_id"`
	RequestHash []byte `db:"request_hash"`
	ExpiresAt   int64  `db:"expires_at"`
}

// ClaimMediaIdempotencyParams describes one idempotency reservation for a
// CreateUpload attempt.
type ClaimMediaIdempotencyParams struct {
	ActorUserID    int64
	Operation      string
	IdempotencyKey string
	RequestHash    []byte
	AssetID        int64
	CreatedAt      int64
	ExpiresAt      int64
}

// MediaIdempotencyClaim is the result of reserving or reading an idempotency
// key. Claimed is true when the caller created the reservation.
type MediaIdempotencyClaim struct {
	AssetID     int64
	RequestHash []byte
	Claimed     bool
}

// ClaimMediaIdempotency atomically reserves an idempotency key or returns the
// existing reservation. Expired reservations are removed lazily for the
// requested key so that it can represent a new creation intent.
func (s *SQLStore) ClaimMediaIdempotency(
	ctx context.Context,
	params ClaimMediaIdempotencyParams,
) (*MediaIdempotencyClaim, error) {
	for range 3 {
		if _, err := s.q.ExecContext(
			ctx,
			DeleteExpiredMediaIdempotencyStatement,
			params.ActorUserID,
			params.Operation,
			params.IdempotencyKey,
			params.CreatedAt,
		); err != nil {
			return nil, err
		}

		row := &mediaIdempotencyRow{}
		rows, err := sqlx.NamedQueryContext(ctx, s.q, ClaimMediaIdempotencyQuery, map[string]any{
			"actor_user_id":   params.ActorUserID,
			"operation":       params.Operation,
			"idempotency_key": params.IdempotencyKey,
			"request_hash":    params.RequestHash,
			"asset_id":        params.AssetID,
			"created_at":      params.CreatedAt,
			"expires_at":      params.ExpiresAt,
		})
		if err != nil {
			return nil, err
		}
		inserted := rows.Next()
		if inserted {
			err = rows.StructScan(row)
		} else {
			err = rows.Err()
		}
		closeErr := rows.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if inserted {
			return &MediaIdempotencyClaim{
				AssetID:     row.AssetID,
				RequestHash: append([]byte(nil), row.RequestHash...),
				Claimed:     true,
			}, nil
		}

		if err := sqlx.GetContext(
			ctx,
			s.q,
			row,
			GetMediaIdempotencyQuery,
			params.ActorUserID,
			params.Operation,
			params.IdempotencyKey,
		); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return nil, err
		}
		if row.ExpiresAt <= params.CreatedAt {
			continue
		}
		return &MediaIdempotencyClaim{
			AssetID:     row.AssetID,
			RequestHash: append([]byte(nil), row.RequestHash...),
		}, nil
	}
	return nil, sql.ErrNoRows
}
