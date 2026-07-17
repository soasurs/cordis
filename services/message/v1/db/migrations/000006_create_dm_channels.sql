-- Private 1:1 conversations. Participants are stored in ascending user ID
-- order so each pair maps to exactly one channel.
CREATE TABLE IF NOT EXISTS dm_channels (
    id          BIGINT PRIMARY KEY CHECK (id > 0),
    user_lo     BIGINT NOT NULL CHECK (user_lo > 0),
    user_hi     BIGINT NOT NULL CHECK (user_hi > user_lo),
    created_at  BIGINT NOT NULL CHECK (created_at > 0),
    UNIQUE (user_lo, user_hi)
);

CREATE INDEX IF NOT EXISTS dm_channels_user_hi_idx
    ON dm_channels (user_hi, id DESC);

CREATE INDEX IF NOT EXISTS dm_channels_user_lo_idx
    ON dm_channels (user_lo, id DESC);
