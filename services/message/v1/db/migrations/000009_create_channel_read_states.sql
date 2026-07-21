CREATE TABLE IF NOT EXISTS channel_read_states (
    user_id              BIGINT NOT NULL CHECK (user_id > 0),
    channel_id           BIGINT NOT NULL CHECK (channel_id > 0),
    last_read_message_id BIGINT NOT NULL DEFAULT 0 CHECK (last_read_message_id >= 0),
    updated_at           BIGINT NOT NULL DEFAULT 0 CHECK (updated_at >= 0),
    PRIMARY KEY (user_id, channel_id)
);
