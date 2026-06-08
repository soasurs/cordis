// Package outbox provides a transactional outbox for reliably publishing
// events to Kafka. Events are inserted in the same database transaction as
// business data, then asynchronously flushed to Kafka after commit.
//
// Each service that needs the outbox pattern creates its own outbox_messages
// table and uses this package to manage the lifecycle.
package outbox

import "encoding/json"

// Event is a Kafka message waiting to be sent.
// It is stored in the outbox_messages table within the same transaction
// as the business data that produced it.
type Event struct {
	ID int64 // snowflake, primary key

	// Topic is the Kafka topic to publish to (e.g. "message.events").
	Topic string
	// Key is the Kafka message key used for partition routing.
	// For message events, this is typically the channel_id serialized as a string.
	Key []byte
	// Payload is the Kafka message value — the serialized JSON event body.
	Payload json.RawMessage

	RetryCount int
	MaxRetries int

	// LockedAt is a unix-millis timestamp.
	// 0 means the event is pending and available for claiming.
	// > 0 means the event is being processed by a worker identified by LockedBy.
	LockedAt int64

	CreatedAt int64 // unix-millis
}

// TableSQL returns the CREATE TABLE statement for outbox_messages.
// Each service should embed this in its own migration.
const TableSQL = `
CREATE TABLE IF NOT EXISTS outbox_messages (
	id           BIGINT PRIMARY KEY CHECK (id > 0),
	topic        TEXT NOT NULL CHECK (topic <> ''),
	key          BYTEA,
	payload      JSONB NOT NULL,
	retry_count  INT NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
	max_retries  INT NOT NULL DEFAULT 5 CHECK (max_retries >= 0),
	locked_at    BIGINT NOT NULL DEFAULT 0 CHECK (locked_at >= 0),
	created_at   BIGINT NOT NULL CHECK (created_at > 0)
);

CREATE INDEX IF NOT EXISTS idx_outbox_messages_fetch
	ON outbox_messages (id)
	WHERE locked_at = 0;

CREATE INDEX IF NOT EXISTS idx_outbox_messages_locked
	ON outbox_messages (locked_at)
	WHERE locked_at > 0;
`
