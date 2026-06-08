package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// Queryer abstracts *sqlx.DB / *sqlx.Tx so that Store functions can be
// called both inside and outside a transaction.
type Queryer = sqlx.ExtContext

// Insert records a new outbox event. Call this inside a transaction
// alongside the business data that produced the event.
func Insert(ctx context.Context, q Queryer, evt Event) error {
	query := `
	INSERT INTO outbox_messages (
		id, topic, key, payload, retry_count, max_retries,
		locked_at, deleted_at, created_at
	) VALUES (
		:id, :topic, :key, :payload, :retry_count, :max_retries,
		:locked_at, :deleted_at, :created_at
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
		DeletedAt:  evt.DeletedAt,
		CreatedAt:  evt.CreatedAt,
	})
	return err
}

// ClaimBatch claims up to limit pending events, locking them with the
// current timestamp. Uses FOR UPDATE SKIP LOCKED so concurrent relay
// workers don't block each other.
func ClaimBatch(ctx context.Context, q Queryer, now int64, limit int) ([]Event, error) {
	return claim(ctx, q, now, limit, "")
}

func claim(ctx context.Context, q Queryer, now int64, limit int, extraWhere string, extraArgs ...any) ([]Event, error) {
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
	RETURNING id, topic, key, payload, retry_count, max_retries, locked_at, deleted_at, created_at
	`, extraWhere, len(args))

	var rows []eventRow
	if err := sqlx.SelectContext(ctx, q, &rows, query, args...); err != nil {
		return nil, err
	}
	return toEvents(rows), nil
}

// Release returns an event to the pending state and increments its retry
// count. Use when Kafka delivery failed and the event should be retried.
func Release(ctx context.Context, q Queryer, id int64) error {
	_, err := q.ExecContext(ctx, `
	UPDATE outbox_messages
	SET locked_at = 0, retry_count = retry_count + 1
	WHERE id = $1
	`, id)
	return err
}

// MarkSent marks an event as successfully published to Kafka.
// The row stays in the table for retention-period cleanup.
func MarkSent(ctx context.Context, q Queryer, id, now int64) error {
	_, err := q.ExecContext(ctx, `
	UPDATE outbox_messages
	SET deleted_at = $1
	WHERE id = $2
	`, now, id)
	return err
}

// Cleanup removes events that were successfully sent and are past the
// retention period. Batch size limits the number of rows deleted at once
// to keep autovacuum overhead manageable.
func Cleanup(ctx context.Context, q Queryer, olderThan int64, batchSize int) (int64, error) {
	res, err := q.ExecContext(ctx, `
	DELETE FROM outbox_messages
	WHERE deleted_at > 0 AND deleted_at < $1
	LIMIT $2
	`, olderThan, batchSize)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// RecoverStale resets events that have been locked for longer than
// staleBefore back to pending. This handles the case where a relay worker
// crashes while holding a lock.
func RecoverStale(ctx context.Context, q Queryer, staleBefore int64) error {
	_, err := q.ExecContext(ctx, `
	UPDATE outbox_messages
	SET locked_at = 0
	WHERE locked_at > 0
	  AND locked_at < $1
	  AND retry_count < max_retries
	`, staleBefore)
	return err
}

// DropDead removes events that have exhausted their retries and are past
// the given time threshold, returning the count of removed rows.
func DropDead(ctx context.Context, q Queryer, olderThan int64) (int64, error) {
	res, err := q.ExecContext(ctx, `
	DELETE FROM outbox_messages
	WHERE retry_count >= max_retries AND created_at < $1
	`, olderThan)
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
	DeletedAt  int64  `db:"deleted_at"`
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
			DeletedAt:  r.DeletedAt,
			CreatedAt:  r.CreatedAt,
		})
	}
	return events
}

// Now returns the current unix-millis timestamp.
func Now() int64 {
	return time.Now().UnixMilli()
}
