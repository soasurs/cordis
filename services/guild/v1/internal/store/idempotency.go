package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ErrIdempotencyContention reports that an idempotency claim could not be
// settled after repeated attempts and the caller should retry the request.
var ErrIdempotencyContention = errors.New("idempotency claim contention")

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
		if _, err := s.q.Exec(
			ctx,
			deleteExpiredGuildIdempotencyStatement,
			params.ActorUserID,
			params.Operation,
			params.IdempotencyKey,
			params.CreatedAt,
		); err != nil {
			return nil, err
		}

		row, err := scanOne(ctx, s.q, claimGuildIdempotencyQuery, pgx.RowToStructByNameLax[guildIdempotencyRow],
			params.ActorUserID,
			params.Operation,
			params.IdempotencyKey,
			params.RequestHash,
			params.ResourceID,
			params.CreatedAt,
			params.ExpiresAt,
		)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if err == nil {
			return &GuildIdempotencyClaim{
				ResourceID:  row.ResourceID,
				RequestHash: append([]byte(nil), row.RequestHash...),
				Claimed:     true,
			}, nil
		}

		existing, err := scanOne(ctx, s.q, getGuildIdempotencyQuery, pgx.RowToStructByName[guildIdempotencyRow],
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
		return &GuildIdempotencyClaim{
			ResourceID:  existing.ResourceID,
			RequestHash: append([]byte(nil), existing.RequestHash...),
		}, nil
	}
	return nil, ErrIdempotencyContention
}
