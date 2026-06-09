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

CREATE INDEX IF NOT EXISTS idx_outbox_fetch
    ON outbox_messages (partition_id, id)
    WHERE locked_at = 0 AND dead_at = 0 AND deleted_at = 0;

CREATE INDEX IF NOT EXISTS idx_outbox_cleanup
    ON outbox_messages (deleted_at)
    WHERE deleted_at > 0;
