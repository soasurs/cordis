CREATE TABLE IF NOT EXISTS guild_bans (
    guild_id       BIGINT NOT NULL CHECK (guild_id > 0),
    user_id        BIGINT NOT NULL CHECK (user_id > 0),
    actor_user_id  BIGINT NOT NULL CHECK (actor_user_id > 0),
    reason         TEXT NOT NULL DEFAULT '' CHECK (char_length(reason) <= 512),
    created_at     BIGINT NOT NULL CHECK (created_at > 0),
    PRIMARY KEY (guild_id, user_id)
);

CREATE INDEX IF NOT EXISTS guild_bans_guild_user_idx
    ON guild_bans (guild_id, user_id DESC);
