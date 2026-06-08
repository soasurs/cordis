package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// Queryer abstracts *sqlx.DB / *sqlx.Tx so that Store methods can be called
// both inside and outside a transaction.
type Queryer = sqlx.ExtContext

// Insert records a new outbox event. Call this inside a transaction alongside
// the business operation that produced the event.
func Insert(ctx context.Context, q Queryer, evt Event) error {
	query := `
	INSERT INTO outbox_messages (
		id, topic, key, payload, retry_count, max_retries, locked_at, created_at
	) VALUES (
		:id, :topic, :key, :payload, :retry_count, :max_retries, :locked_at, :created_at
	)
	`
	_, err := sqlx.NamedExecContext(ctx, q, query, eventRow{
		ID:         evt.ID,
		Topic:      evt.Topic,
		Key:        evt.Key,
		Payload:    evt.Payload,
		RetryCount: evt.RetryCount,
		MaxRetries: evt.MaxRetries,
		LockedAt:   evt.LockedAt,
		CreatedAt:  evt.CreatedAt,
	})
	return err
}

// ClaimOne attempts to claim a single outbox event by ID using CAS semantics.
// Returns the event if claimed, nil if another worker already claimed it.
func ClaimOne(ctx context.Context, q Queryer, id int64, now int64) (*Event, error) {
	events, err := claim(ctx, q, now, 1, `AND id = $2`, id)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, nil
	}
	return &events[0], nil
}

// ClaimBatch attempts to claim up to limit pending events.
// Uses FOR UPDATE SKIP LOCKED so multiple workers can claim concurrently
// without blocking each other.
func ClaimBatch(ctx context.Context, q Queryer, now int64, limit int) ([]Event, error) {
	return claim(ctx, q, now, limit, "")
}

func claim(ctx context.Context, q Queryer, now int64, limit int, extraWhere string, extraArgs ...any) ([]Event, error) {
	// positional: $1 = locked_at new value
	// extraWhere may add $2, ... — so we pass now last in the extraArgs
	args := []any{now}
	args = append(args, extraArgs...)
	args = append(args, limit)

	query := fmt.Sprintf(`
	UPDATE outbox_messages
	SET locked_at = $1
	WHERE id IN (
		SELECT id FROM outbox_messages
		WHERE locked_at = 0
		%s
		ORDER BY id
		LIMIT $%d
		FOR UPDATE SKIP LOCKED
	)
	RETURNING id, topic, key, payload, retry_count, max_retries, locked_at, created_at
	`, extraWhere, len(args))

	var rows []eventRow
	if err := sqlx.SelectContext(ctx, q, &rows, query, args...); err != nil {
		return nil, err
	}
	return toEvents(rows), nil
}

// Release returns an event to the pending state and increments its retry
// count. Use this when Kafka delivery failed and the event should be retried
// later.
func Release(ctx context.Context, q Queryer, id int64) error {
	query := `
	UPDATE outbox_messages
	SET locked_at = 0, retry_count = retry_count + 1
	WHERE id = $1
	`
	_, err := q.ExecContext(ctx, query, id)
	return err
}

// Delete removes an event from the outbox table. Call this after the event
// has been successfully published to Kafka.
func Delete(ctx context.Context, q Queryer, id int64) error {
	query := `DELETE FROM outbox_messages WHERE id = $1`
	_, err := q.ExecContext(ctx, query, id)
	return err
}

// RecoverStale resets events that have been locked for longer than
// staleBefore back to the pending state, unless they have exhausted their
// retries. This prevents events from being stuck if a worker crashes while
// processing.
func RecoverStale(ctx context.Context, q Queryer, staleBefore int64) error {
	query := `
	UPDATE outbox_messages
	SET locked_at = 0
	WHERE locked_at > 0
	  AND locked_at < $1
	  AND retry_count < max_retries
	`
	_, err := q.ExecContext(ctx, query, staleBefore)
	return err
}

// DropDead removes events that have exhausted their retries and are past the
// given retention period. These are events that failed to publish even after
// multiple retries and should be logged/alerted on.
func DropDead(ctx context.Context, q Queryer, olderThan int64) (int64, error) {
	res, err := q.ExecContext(ctx, `DELETE FROM outbox_messages WHERE retry_count >= max_retries AND created_at < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// --- Internal row types ---

type eventRow struct {
	ID         int64  `db:"id"`
	Topic      string `db:"topic"`
	Key        []byte `db:"key"`
	Payload    []byte `db:"payload"`
	RetryCount int    `db:"retry_count"`
	MaxRetries int    `db:"max_retries"`
	LockedAt   int64  `db:"locked_at"`
	CreatedAt  int64  `db:"created_at"`
}

func toEvents(rows []eventRow) []Event {
	events := make([]Event, 0, len(rows))
	for _, r := range rows {
		events = append(events, Event{
			ID:         r.ID,
			Topic:      r.Topic,
			Key:        r.Key,
			Payload:    r.Payload,
			RetryCount: r.RetryCount,
			MaxRetries: r.MaxRetries,
			LockedAt:   r.LockedAt,
			CreatedAt:  r.CreatedAt,
		})
	}
	return events
}

// Now returns the current unix-millis timestamp. A thin wrapper so all
// outbox operations use the same time source.
func Now() int64 {
	return time.Now().UnixMilli()
}
