package outbox

import (
	"context"
	"database/sql"
	"sort"
	"time"
)

// Queryer is implemented by *sqlx.DB, *sqlx.Tx, and *sql.Conn.
type Queryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// Insert records a new outbox event. Call this inside a transaction
// alongside the business data that produced the event.
func Insert(ctx context.Context, q Queryer, evt Event) error {
	query := `
	INSERT INTO outbox_messages (
		id, topic, key, partition_id, payload, retry_count, max_retries,
		available_at, locked_at, dead_at, deleted_at, created_at
	) VALUES (
		$1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10, $11, $12
	)
	`
	_, err := q.ExecContext(
		ctx,
		query,
		evt.ID,
		evt.Topic,
		evt.Key,
		evt.Partition,
		[]byte(evt.Payload),
		evt.RetryCount,
		evt.MaxRetries,
		evt.AvailableAt,
		evt.LockedAt,
		evt.DeadAt,
		evt.DeletedAt,
		evt.CreatedAt,
	)
	return err
}

// ClaimBatch claims up to limit pending events from one partition, locking
// them with the current timestamp. The caller must hold that partition's
// advisory lock until publishing and state updates complete.
func ClaimBatch(ctx context.Context, q Queryer, now int64, limit, partition int) ([]Event, error) {
	query := `
	UPDATE outbox_messages AS claimed
	SET locked_at = $1
	WHERE id IN (
		SELECT candidate.id
		FROM outbox_messages AS candidate
		WHERE candidate.locked_at = 0
		  AND candidate.dead_at = 0
		  AND candidate.deleted_at = 0
		  AND candidate.retry_count <= candidate.max_retries
		  AND candidate.available_at <= $1
		  AND candidate.partition_id = $2
		ORDER BY candidate.id
		LIMIT $3
		FOR UPDATE OF candidate SKIP LOCKED
	)
	RETURNING id, topic, key, partition_id, payload, retry_count, max_retries,
		available_at, locked_at, dead_at, deleted_at, created_at
	`

	result, err := q.QueryContext(ctx, query, now, partition, limit)
	if err != nil {
		return nil, err
	}
	defer result.Close()

	var rows []eventRow
	for result.Next() {
		var row eventRow
		if err := result.Scan(
			&row.ID,
			&row.Topic,
			&row.Key,
			&row.Partition,
			&row.Payload,
			&row.RetryCount,
			&row.MaxRetries,
			&row.AvailableAt,
			&row.LockedAt,
			&row.DeadAt,
			&row.DeletedAt,
			&row.CreatedAt,
		); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	if err := result.Err(); err != nil {
		return nil, err
	}
	events := toEvents(rows)
	sort.Slice(events, func(i, j int) bool {
		return events[i].ID < events[j].ID
	})
	return events, nil
}

// ReadyPartitions returns partitions with claimable events. Relay workers use
// this to avoid probing every advisory lock when the outbox is idle.
func ReadyPartitions(ctx context.Context, q Queryer, now int64, limit int) ([]int, error) {
	rows, err := q.QueryContext(ctx, `
	SELECT partition_id
	FROM outbox_messages
	WHERE locked_at = 0
	  AND dead_at = 0
	  AND deleted_at = 0
	  AND retry_count <= max_retries
	  AND available_at <= $1
	GROUP BY partition_id
	ORDER BY partition_id
	LIMIT $2
	`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	partitions := make([]int, 0, limit)
	for rows.Next() {
		var partition int
		if err := rows.Scan(&partition); err != nil {
			return nil, err
		}
		partitions = append(partitions, partition)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return partitions, nil
}

// Release records a failed attempt. Events below max_retries are returned to
// pending with retry backoff; exhausted events are retained as dead records.
func Release(ctx context.Context, q Queryer, id, now, nextAttemptAt int64) error {
	_, err := q.ExecContext(ctx, `
	UPDATE outbox_messages
	SET
		locked_at = 0,
		retry_count = retry_count + 1,
		available_at = CASE
			WHEN retry_count + 1 <= max_retries THEN $2
			ELSE available_at
		END,
		dead_at = CASE
			WHEN retry_count + 1 > max_retries THEN $1
			ELSE dead_at
		END
	WHERE id = $3 AND deleted_at = 0
	`, now, nextAttemptAt, id)
	return err
}

// MarkSent marks an event as successfully published to Kafka.
// The row stays in the table for retention-period cleanup.
func MarkSent(ctx context.Context, q Queryer, id, now int64) error {
	_, err := q.ExecContext(ctx, `
	UPDATE outbox_messages
	SET deleted_at = $1, locked_at = 0
	WHERE id = $2 AND dead_at = 0
	`, now, id)
	return err
}

// Cleanup removes events that were successfully sent and are past the
// retention period. Batch size limits the number of rows deleted at once
// to keep autovacuum overhead manageable.
func Cleanup(ctx context.Context, q Queryer, olderThan int64, batchSize int) (int64, error) {
	res, err := q.ExecContext(ctx, `
	WITH expired AS (
		SELECT id
		FROM outbox_messages
		WHERE deleted_at > 0 AND deleted_at < $1
		ORDER BY deleted_at
		LIMIT $2
	)
	DELETE FROM outbox_messages
	WHERE id IN (SELECT id FROM expired)
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
func RecoverStale(ctx context.Context, q Queryer, staleBefore int64, partition int) error {
	_, err := q.ExecContext(ctx, `
	UPDATE outbox_messages
	SET locked_at = 0
	WHERE locked_at > 0
	  AND locked_at < $1
	  AND dead_at = 0
	  AND deleted_at = 0
	  AND retry_count <= max_retries
	  AND partition_id = $2
	`, staleBefore, partition)
	return err
}

// --- Internal row types ---

type eventRow struct {
	ID          int64  `db:"id"`
	Topic       string `db:"topic"`
	Key         []byte `db:"key"`
	Partition   int    `db:"partition_id"`
	Payload     []byte `db:"payload"`
	RetryCount  int    `db:"retry_count"`
	MaxRetries  int    `db:"max_retries"`
	AvailableAt int64  `db:"available_at"`
	LockedAt    int64  `db:"locked_at"`
	DeadAt      int64  `db:"dead_at"`
	DeletedAt   int64  `db:"deleted_at"`
	CreatedAt   int64  `db:"created_at"`
}

func toEvents(rows []eventRow) []Event {
	events := make([]Event, 0, len(rows))
	for _, r := range rows {
		events = append(events, Event{
			ID:          r.ID,
			Topic:       r.Topic,
			Key:         r.Key,
			Partition:   r.Partition,
			Payload:     r.Payload,
			RetryCount:  r.RetryCount,
			MaxRetries:  r.MaxRetries,
			AvailableAt: r.AvailableAt,
			LockedAt:    r.LockedAt,
			DeadAt:      r.DeadAt,
			DeletedAt:   r.DeletedAt,
			CreatedAt:   r.CreatedAt,
		})
	}
	return events
}

// Now returns the current unix-millis timestamp.
func Now() int64 {
	return time.Now().UnixMilli()
}
