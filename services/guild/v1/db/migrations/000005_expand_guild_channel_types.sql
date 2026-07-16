ALTER TABLE guild_channels
    DROP CONSTRAINT IF EXISTS guild_channels_type_check;

ALTER TABLE guild_channels
    ADD CONSTRAINT guild_channels_type_check CHECK (type IN (1, 2, 3));

ALTER TABLE guild_channels
    ADD COLUMN IF NOT EXISTS parent_id BIGINT NOT NULL DEFAULT 0 CHECK (parent_id >= 0);

CREATE INDEX IF NOT EXISTS guild_channels_parent_idx
    ON guild_channels (guild_id, parent_id, position, id)
    WHERE deleted_at = 0;
