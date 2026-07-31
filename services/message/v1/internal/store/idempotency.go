package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
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
		if _, err := s.q.ExecContext(
			ctx,
			DeleteExpiredMessageIdempotencyStatement,
			params.ActorUserID,
			params.Operation,
			params.IdempotencyKey,
			params.CreatedAt,
		); err != nil {
			return nil, err
		}

		row := &messageIdempotencyRow{}
		rows, err := sqlx.NamedQueryContext(ctx, s.q, ClaimMessageIdempotencyQuery, map[string]any{
			"actor_user_id":   params.ActorUserID,
			"operation":       params.Operation,
			"idempotency_key": params.IdempotencyKey,
			"request_hash":    params.RequestHash,
			"message_id":      params.MessageID,
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
			return &MessageIdempotencyClaim{
				MessageID:   row.MessageID,
				RequestHash: append([]byte(nil), row.RequestHash...),
				Claimed:     true,
			}, nil
		}

		if err := sqlx.GetContext(
			ctx,
			s.q,
			row,
			GetMessageIdempotencyQuery,
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
		return &MessageIdempotencyClaim{
			MessageID:   row.MessageID,
			RequestHash: append([]byte(nil), row.RequestHash...),
		}, nil
	}
	return nil, ErrIdempotencyContention
}
