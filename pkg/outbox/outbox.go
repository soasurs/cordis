// Package outbox provides a transactional outbox for reliably publishing
// events to Kafka. Events are inserted in the same database transaction as
// business data, then asynchronously flushed to Kafka after commit via a
// background relay.
//
// Each service that needs the outbox pattern creates its own outbox_messages
// table and uses this package to manage the lifecycle.
package outbox

import (
	"encoding/json"
	"hash/fnv"
	"strconv"
)

// DefaultPartitionCount is the default number of logical outbox partitions.
// It may be overridden per-service via [RelayConfig.PartitionCount].
// Changing the partition count at runtime requires an explicit data migration
// before deploying writers.
const DefaultPartitionCount = 64

// Event is a Kafka message waiting to be sent.
type Event struct {
	ID        int64 // snowflake, primary key and ordering cursor
	Topic     string
	Key       []byte // Kafka message key for partition routing
	Partition int    // logical outbox partition used for relay ownership
	// Payload is the Kafka message value — the serialized JSON event body.
	Payload    json.RawMessage
	RetryCount int
	MaxRetries int
	// AvailableAt is the earliest unix-millis timestamp at which the event
	// may be claimed. It provides retry backoff after delivery failures.
	AvailableAt int64
	// LockedAt is a unix-millis timestamp.
	// 0 = pending, > 0 = being processed by a relay worker.
	LockedAt int64
	// DeadAt is a unix-millis timestamp.
	// 0 = active, > 0 = delivery attempts exhausted and retained for repair.
	DeadAt int64
	// DeletedAt is a unix-millis timestamp.
	// 0 = not yet sent, > 0 = successfully published and waiting cleanup.
	DeletedAt int64
	CreatedAt int64
}

// TableSQL returns the CREATE TABLE statement for outbox_messages.
// Each service should embed this in its own migration.
const TableSQL = `
	CREATE TABLE IF NOT EXISTS outbox_messages (
		id           BIGINT PRIMARY KEY CHECK (id > 0),
		topic        TEXT NOT NULL CHECK (topic <> ''),
		key          BYTEA,
		partition_id INT NOT NULL DEFAULT 0 CHECK (partition_id >= 0),
		payload      JSONB NOT NULL,
		retry_count  INT NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
		max_retries  INT NOT NULL DEFAULT 5 CHECK (max_retries >= 0),
		available_at BIGINT NOT NULL DEFAULT 0 CHECK (available_at >= 0),
		locked_at    BIGINT NOT NULL DEFAULT 0 CHECK (locked_at >= 0),
		dead_at      BIGINT NOT NULL DEFAULT 0 CHECK (dead_at >= 0),
		deleted_at   BIGINT NOT NULL DEFAULT 0 CHECK (deleted_at >= 0),
		created_at   BIGINT NOT NULL CHECK (created_at > 0)
	);

	-- Relay dispatcher: finds pending events in the partitions owned by a worker.
	CREATE INDEX IF NOT EXISTS idx_outbox_fetch
		ON outbox_messages (partition_id, id)
		WHERE locked_at = 0 AND dead_at = 0 AND deleted_at = 0;

	-- Cleanup: finds sent events past their retention to delete.
	CREATE INDEX IF NOT EXISTS idx_outbox_cleanup
		ON outbox_messages (deleted_at)
		WHERE deleted_at > 0;
`

// PartitionForKey deterministically maps a routing key to a logical outbox
// partition in the range [0, partitionCount).
func PartitionForKey(key []byte, partitionCount int) int {
	if value, err := strconv.ParseUint(string(key), 10, 64); err == nil {
		return int(value % uint64(partitionCount))
	}
	hash := fnv.New32a()
	_, _ = hash.Write(key)
	return int(hash.Sum32() % uint32(partitionCount))
}
