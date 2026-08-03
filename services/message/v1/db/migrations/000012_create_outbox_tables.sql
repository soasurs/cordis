CREATE TABLE IF NOT EXISTS message_event_streams (
    stream_key     TEXT PRIMARY KEY,
    relay_shard_id INT NOT NULL CHECK (relay_shard_id >= 0),
    last_sequence  BIGINT NOT NULL DEFAULT 0 CHECK (last_sequence >= 0)
);

CREATE TABLE IF NOT EXISTS message_outbox_events (
    outbox_id        BIGINT PRIMARY KEY CHECK (outbox_id > 0),
    event_id         BIGINT NOT NULL CHECK (event_id > 0),
    delivery_index   INT NOT NULL DEFAULT 0 CHECK (delivery_index >= 0),
    stream_key       TEXT NOT NULL,
    relay_shard_id   INT NOT NULL CHECK (relay_shard_id >= 0),
    stream_sequence  BIGINT NOT NULL CHECK (stream_sequence > 0),
    topic            TEXT NOT NULL CHECK (topic <> ''),
    event_type       TEXT NOT NULL CHECK (event_type <> ''),
    key              BYTEA NOT NULL,
    payload          BYTEA NOT NULL,
    trace_context    BYTEA NOT NULL DEFAULT ''::BYTEA,
    attempts         INT NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at  BIGINT NOT NULL DEFAULT 0 CHECK (next_attempt_at >= 0),
    created_at       BIGINT NOT NULL CHECK (created_at > 0),
    UNIQUE (stream_key, stream_sequence),
    UNIQUE (event_id, delivery_index)
);

CREATE INDEX IF NOT EXISTS idx_message_outbox_fetch
    ON message_outbox_events (relay_shard_id, stream_key, stream_sequence);

CREATE TABLE IF NOT EXISTS read_state_event_streams (
    stream_key     TEXT PRIMARY KEY,
    relay_shard_id INT NOT NULL CHECK (relay_shard_id >= 0),
    last_sequence  BIGINT NOT NULL DEFAULT 0 CHECK (last_sequence >= 0)
);

CREATE TABLE IF NOT EXISTS read_state_outbox_events (
    outbox_id        BIGINT PRIMARY KEY CHECK (outbox_id > 0),
    event_id         BIGINT NOT NULL CHECK (event_id > 0),
    delivery_index   INT NOT NULL DEFAULT 0 CHECK (delivery_index >= 0),
    stream_key       TEXT NOT NULL,
    relay_shard_id   INT NOT NULL CHECK (relay_shard_id >= 0),
    stream_sequence  BIGINT NOT NULL CHECK (stream_sequence > 0),
    topic            TEXT NOT NULL CHECK (topic <> ''),
    event_type       TEXT NOT NULL CHECK (event_type <> ''),
    key              BYTEA NOT NULL,
    payload          BYTEA NOT NULL,
    trace_context    BYTEA NOT NULL DEFAULT ''::BYTEA,
    attempts         INT NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at  BIGINT NOT NULL DEFAULT 0 CHECK (next_attempt_at >= 0),
    created_at       BIGINT NOT NULL CHECK (created_at > 0),
    UNIQUE (stream_key, stream_sequence),
    UNIQUE (event_id, delivery_index)
);

CREATE INDEX IF NOT EXISTS idx_read_state_outbox_fetch
    ON read_state_outbox_events (relay_shard_id, stream_key, stream_sequence);
