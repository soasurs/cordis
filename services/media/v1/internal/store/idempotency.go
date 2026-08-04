package store

import (
	"context"
	"database/sql"

	"github.com/jackc/pgx/v5"
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
		if _, err := s.q.Exec(
			ctx,
			DeleteExpiredMediaIdempotencyStatement,
			params.ActorUserID,
			params.Operation,
			params.IdempotencyKey,
			params.CreatedAt,
		); err != nil {
			return nil, err
		}

		row, err := scanOne(ctx, s.q, ClaimMediaIdempotencyQuery, pgx.RowToStructByNameLax[mediaIdempotencyRow],
			params.ActorUserID,
			params.Operation,
			params.IdempotencyKey,
			params.RequestHash,
			params.AssetID,
			params.CreatedAt,
			params.ExpiresAt,
		)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if err == nil {
			return &MediaIdempotencyClaim{
				AssetID:     row.AssetID,
				RequestHash: append([]byte(nil), row.RequestHash...),
				Claimed:     true,
			}, nil
		}

		existing, err := scanOne(ctx, s.q, GetMediaIdempotencyQuery, pgx.RowToStructByName[mediaIdempotencyRow],
			params.ActorUserID,
			params.Operation,
			params.IdempotencyKey,
		)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return nil, err
		}
		if existing.ExpiresAt <= params.CreatedAt {
			continue
		}
		return &MediaIdempotencyClaim{
			AssetID:     existing.AssetID,
			RequestHash: append([]byte(nil), existing.RequestHash...),
		}, nil
	}
	return nil, sql.ErrNoRows
}
