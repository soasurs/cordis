package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

type guildIdempotencyRow struct {
	ResourceID  int64  `db:"resource_id"`
	RequestHash []byte `db:"request_hash"`
	ExpiresAt   int64  `db:"expires_at"`
}

// ClaimGuildIdempotency atomically reserves an idempotency key or returns
// the existing reservation. Expired reservations are removed lazily for the
// requested key so that it can represent a new creation intent.
func (s *SQLStore) ClaimGuildIdempotency(
	ctx context.Context,
	params ClaimGuildIdempotencyParams,
) (*GuildIdempotencyClaim, error) {
	for range 3 {
		if _, err := s.q.ExecContext(
			ctx,
			deleteExpiredGuildIdempotencyStatement,
			params.ActorUserID,
			params.Operation,
			params.IdempotencyKey,
			params.CreatedAt,
		); err != nil {
			return nil, err
		}

		row := &guildIdempotencyRow{}
		rows, err := sqlx.NamedQueryContext(ctx, s.q, claimGuildIdempotencyQuery, map[string]any{
			"actor_user_id":   params.ActorUserID,
			"operation":       params.Operation,
			"idempotency_key": params.IdempotencyKey,
			"request_hash":    params.RequestHash,
			"resource_id":     params.ResourceID,
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
			return &GuildIdempotencyClaim{
				ResourceID:  row.ResourceID,
				RequestHash: append([]byte(nil), row.RequestHash...),
				Claimed:     true,
			}, nil
		}

		if err := sqlx.GetContext(
			ctx,
			s.q,
			row,
			getGuildIdempotencyQuery,
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
		return &GuildIdempotencyClaim{
			ResourceID:  row.ResourceID,
			RequestHash: append([]byte(nil), row.RequestHash...),
		}, nil
	}
	return nil, sql.ErrNoRows
}
