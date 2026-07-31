ALTER TABLE guilds
    ADD COLUMN IF NOT EXISTS channel_layout_revision BIGINT NOT NULL DEFAULT 1
    CHECK (channel_layout_revision > 0);
