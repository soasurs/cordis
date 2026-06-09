DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'outbox_messages'
          AND column_name = 'partition_id'
    ) THEN
        ALTER TABLE outbox_messages
            ADD COLUMN partition_id INT NOT NULL DEFAULT 0
                CHECK (partition_id >= 0);

        -- Existing message outbox keys are decimal channel IDs. Repartition
        -- them using the default fixed partition count so old and new events
        -- for one channel retain the same ownership boundary.
        UPDATE outbox_messages
        SET partition_id = (
            convert_from(key, 'UTF8')::NUMERIC % 64
        )::INT
        WHERE key IS NOT NULL
          AND convert_from(key, 'UTF8') ~ '^[0-9]+$';
    END IF;
END
$$;

DROP INDEX IF EXISTS idx_outbox_fetch;

CREATE INDEX idx_outbox_fetch
    ON outbox_messages (partition_id, id)
    WHERE locked_at = 0 AND dead_at = 0 AND deleted_at = 0;
