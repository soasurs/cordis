ALTER TABLE outbox_messages
    ADD COLUMN IF NOT EXISTS available_at BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS dead_at BIGINT NOT NULL DEFAULT 0;

DROP INDEX IF EXISTS idx_outbox_fetch;
DROP INDEX IF EXISTS outbox_messages_sequence_key;

CREATE INDEX idx_outbox_fetch
    ON outbox_messages (id)
    WHERE locked_at = 0 AND dead_at = 0 AND deleted_at = 0;

DROP INDEX IF EXISTS idx_outbox_key_sequence;

ALTER TABLE outbox_messages
    DROP COLUMN IF EXISTS sequence;

DROP SEQUENCE IF EXISTS outbox_messages_sequence_seq;
