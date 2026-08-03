// Package outbox provides a domain-agnostic transactional event outbox.
//
// Services embed the outbox tables in their own migrations and use this
// package to reserve per-stream sequences, insert event rows, and let a relay
// select, publish, and acknowledge them. Streams are identified by an opaque
// string key (for example a channel ID or a user/channel pair), so the same
// package can back multiple outboxes in one service.
package outbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/jmoiron/sqlx"
)

// Tables names the outbox tables owned by one service. The names are fixed
// constants supplied by the caller and are safely quoted before use.
type Tables struct {
	// Streams holds one row per ordered stream.
	Streams string
	// Events holds one row per Kafka record.
	Events string
}

// Querier is the query surface used by the package. *sqlx.DB, *sqlx.Tx, and
// *sqlx.Conn all satisfy it, so writers can call these helpers inside a
// service transaction and the relay can call them on a dedicated connection.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryxContext(ctx context.Context, query string, args ...any) (*sqlx.Rows, error)
	QueryRowxContext(ctx context.Context, query string, args ...any) *sqlx.Row
}

// ReservedRange is the sequence range assigned to one transaction.
type ReservedRange struct {
	FirstSequence int64
	LastSequence  int64
	ShardID       int
}

// Record is one outbox row.
type Record struct {
	OutboxID       int64
	EventID        int64
	DeliveryIndex  int
	StreamKey      string
	RelayShardID   int
	StreamSequence int64
	Topic          string
	EventType      string
	Key            []byte
	Payload        []byte
	TraceContext   []byte
	Attempts       int
	NextAttemptAt  int64
	CreatedAt      int64
}

// FailedUpdate is the backoff state for one failed record.
type FailedUpdate struct {
	OutboxID      int64
	Attempts      int
	NextAttemptAt int64
}

// ShardID returns the stable virtual shard for a stream key.
func ShardID(streamKey string, shardCount int) int {
	if shardCount <= 0 {
		panic("outbox: shard count must be positive")
	}
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(streamKey))
	return int(hasher.Sum64() % uint64(shardCount))
}

// EnsureStream inserts a stream row when missing. The caller must call
// ReserveSequences afterwards; ReserveSequences reports sql.ErrNoRows when
// another transaction's uncommitted insert vanished and the insert needs to
// be retried.
func EnsureStream(ctx context.Context, q Querier, streamsTable, streamKey string, shardID int) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (stream_key, relay_shard_id, last_sequence)
		VALUES ($1, $2, 0)
		ON CONFLICT (stream_key) DO NOTHING
	`, quoteIdent(streamsTable))
	_, err := q.ExecContext(ctx, query, streamKey, shardID)
	return err
}

// ReserveSequences atomically reserves count contiguous sequence values for
// one stream. The stream row must already exist; call EnsureStream first.
func ReserveSequences(ctx context.Context, q Querier, streamsTable, streamKey string, count int) (ReservedRange, error) {
	if count <= 0 {
		return ReservedRange{}, errors.New("outbox: sequence count must be positive")
	}
	query := fmt.Sprintf(`
		UPDATE %s
		SET last_sequence = last_sequence + $2
		WHERE stream_key = $1
		RETURNING
			last_sequence - $2 + 1 AS first_sequence,
			last_sequence,
			relay_shard_id
	`, quoteIdent(streamsTable))

	var rangeValue ReservedRange
	err := q.QueryRowxContext(ctx, query, streamKey, count).Scan(
		&rangeValue.FirstSequence,
		&rangeValue.LastSequence,
		&rangeValue.ShardID,
	)
	return rangeValue, err
}

// InsertBatch inserts outbox rows in one statement.
func InsertBatch(ctx context.Context, q Querier, eventsTable string, records []Record) error {
	if len(records) == 0 {
		return nil
	}

	columns := "(outbox_id, event_id, delivery_index, stream_key, relay_shard_id, stream_sequence, topic, event_type, key, payload, trace_context, attempts, next_attempt_at, created_at)"
	var builder strings.Builder
	builder.WriteString("INSERT INTO ")
	builder.WriteString(quoteIdent(eventsTable))
	builder.WriteString(" ")
	builder.WriteString(columns)
	builder.WriteString(" VALUES ")

	args := make([]any, 0, len(records)*14)
	for index, record := range records {
		if index > 0 {
			builder.WriteString(", ")
		}
		offset := index * 14
		builder.WriteString("(")
		for column := range 14 {
			if column > 0 {
				builder.WriteString(", ")
			}
			fmt.Fprintf(&builder, "$%d", offset+column+1)
		}
		builder.WriteString(")")
		args = append(args,
			record.OutboxID,
			record.EventID,
			record.DeliveryIndex,
			record.StreamKey,
			record.RelayShardID,
			record.StreamSequence,
			record.Topic,
			record.EventType,
			record.Key,
			record.Payload,
			traceBytes(record.TraceContext),
			record.Attempts,
			record.NextAttemptAt,
			record.CreatedAt,
		)
	}

	_, err := q.ExecContext(ctx, builder.String(), args...)
	return err
}

func traceBytes(value []byte) []byte {
	if value == nil {
		return []byte{}
	}
	return value
}

// SelectHeads returns at most limit eligible head records, one per stream,
// for one shard. A failed head blocks later records in the same stream
// because next_attempt_at filters the head before any successor is visible.
func SelectHeads(ctx context.Context, q Querier, eventsTable string, shardID int, now, limit int64) ([]Record, error) {
	query := fmt.Sprintf(`
		WITH heads AS (
			SELECT DISTINCT ON (stream_key) *
			FROM %s
			WHERE relay_shard_id = $1
			ORDER BY stream_key, stream_sequence
		)
		SELECT
			outbox_id, event_id, delivery_index, stream_key, relay_shard_id,
			stream_sequence, topic, event_type, key, payload, trace_context,
			attempts, next_attempt_at, created_at
		FROM heads
		WHERE next_attempt_at <= $2
		ORDER BY created_at, outbox_id
		LIMIT $3
	`, quoteIdent(eventsTable))

	rows, err := q.QueryxContext(ctx, query, shardID, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]Record, 0)
	for rows.Next() {
		var record Record
		if err := rows.Scan(
			&record.OutboxID,
			&record.EventID,
			&record.DeliveryIndex,
			&record.StreamKey,
			&record.RelayShardID,
			&record.StreamSequence,
			&record.Topic,
			&record.EventType,
			&record.Key,
			&record.Payload,
			&record.TraceContext,
			&record.Attempts,
			&record.NextAttemptAt,
			&record.CreatedAt,
		); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// DeleteDelivered removes successfully published rows in one statement.
func DeleteDelivered(ctx context.Context, q Querier, eventsTable string, outboxIDs []int64) error {
	if len(outboxIDs) == 0 {
		return nil
	}
	query := fmt.Sprintf(`
		DELETE FROM %s
		WHERE outbox_id = ANY($1)
	`, quoteIdent(eventsTable))
	_, err := q.ExecContext(ctx, query, outboxIDs)
	return err
}

// UpdateFailed applies per-record backoff state in one statement.
func UpdateFailed(ctx context.Context, q Querier, eventsTable string, updates []FailedUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(updates))
	attempts := make([]int32, 0, len(updates))
	nextAttempts := make([]int64, 0, len(updates))
	for _, update := range updates {
		ids = append(ids, update.OutboxID)
		attempts = append(attempts, int32(update.Attempts))
		nextAttempts = append(nextAttempts, update.NextAttemptAt)
	}
	query := fmt.Sprintf(`
		UPDATE %s AS events
		SET attempts = updates.attempts,
			next_attempt_at = updates.next_attempt_at
		FROM unnest($1::bigint[], $2::int[], $3::bigint[]) AS updates(outbox_id, attempts, next_attempt_at)
		WHERE events.outbox_id = updates.outbox_id
	`, quoteIdent(eventsTable))
	_, err := q.ExecContext(ctx, query, ids, attempts, nextAttempts)
	return err
}

// ListReadyShards returns the distinct shards that currently have at least
// one eligible head record.
func ListReadyShards(ctx context.Context, q Querier, eventsTable string, now int64) ([]int, error) {
	query := fmt.Sprintf(`
		SELECT DISTINCT relay_shard_id
		FROM %s
		WHERE next_attempt_at <= $1
		ORDER BY relay_shard_id
	`, quoteIdent(eventsTable))
	rows, err := q.QueryxContext(ctx, query, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	shards := make([]int, 0)
	for rows.Next() {
		var shardID int
		if err := rows.Scan(&shardID); err != nil {
			return nil, err
		}
		shards = append(shards, shardID)
	}
	return shards, rows.Err()
}

// Notify wakes relay listeners after a commit. Because NOTIFY is delivered
// only at commit, calling it inside the writing transaction matches the
// "wake after commit" intent. The channel must be configured on both writer
// and relay; an empty channel disables the call.
func Notify(ctx context.Context, q Querier, channel string) error {
	if channel == "" {
		return nil
	}
	_, err := q.ExecContext(ctx, "SELECT pg_notify($1, '')", channel)
	return err
}

func quoteIdent(name string) string {
	if name == "" {
		panic("outbox: empty table name")
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
