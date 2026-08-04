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

type messageIdempotencyRow struct {
	MessageID   int64  `db:"message_id"`
	RequestHash []byte `db:"request_hash"`
	ExpiresAt   int64  `db:"expires_at"`
}

// ClaimMessageIdempotency atomically reserves an idempotency key or returns
// the existing reservation. Expired reservations are removed lazily for the
// requested key so that it can represent a new creation intent.
func (s *SQLStore) ClaimMessageIdempotency(
	ctx context.Context,
	params ClaimMessageIdempotencyParams,
) (*MessageIdempotencyClaim, error) {
	for range 3 {
		if _, err := s.q.Exec(
			ctx,
			DeleteExpiredMessageIdempotencyStatement,
			params.ActorUserID,
			params.Operation,
			params.IdempotencyKey,
			params.CreatedAt,
		); err != nil {
			return nil, err
		}

		row, err := scanOne(ctx, s.q, ClaimMessageIdempotencyQuery, pgx.RowToStructByNameLax[messageIdempotencyRow], pgx.NamedArgs{
			"actor_user_id":   params.ActorUserID,
			"operation":       params.Operation,
			"idempotency_key": params.IdempotencyKey,
			"request_hash":    params.RequestHash,
			"message_id":      params.MessageID,
			"created_at":      params.CreatedAt,
			"expires_at":      params.ExpiresAt,
		})
		if err == nil {
			return &MessageIdempotencyClaim{
				MessageID:   row.MessageID,
				RequestHash: append([]byte(nil), row.RequestHash...),
				Claimed:     true,
			}, nil
		}
		if err != sql.ErrNoRows {
			return nil, err
		}

		existing, err := scanOne(
			ctx,
			s.q,
			GetMessageIdempotencyQuery,
			pgx.RowToStructByName[messageIdempotencyRow],
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
		return &MessageIdempotencyClaim{
			MessageID:   existing.MessageID,
			RequestHash: append([]byte(nil), existing.RequestHash...),
		}, nil
	}
	return nil, ErrIdempotencyContention
}
