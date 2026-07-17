CREATE TABLE IF NOT EXISTS guild_invites (
    id               BIGINT PRIMARY KEY CHECK (id > 0),
    code             TEXT NOT NULL CHECK (char_length(code) BETWEEN 1 AND 32),
    guild_id         BIGINT NOT NULL CHECK (guild_id > 0),
    creator_user_id  BIGINT NOT NULL CHECK (creator_user_id > 0),
    max_uses         INT NOT NULL DEFAULT 0 CHECK (max_uses >= 0),
    uses             INT NOT NULL DEFAULT 0 CHECK (uses >= 0),
    expires_at       BIGINT NOT NULL DEFAULT 0 CHECK (expires_at >= 0),
    created_at       BIGINT NOT NULL CHECK (created_at > 0),
    CHECK (max_uses = 0 OR uses <= max_uses)
);

CREATE UNIQUE INDEX IF NOT EXISTS guild_invites_code_idx
    ON guild_invites (code);

CREATE INDEX IF NOT EXISTS guild_invites_guild_idx
    ON guild_invites (guild_id, id DESC);

-- Grant CREATE_INVITE (1024) to existing @everyone roles so current guilds
-- keep parity with newly created guilds.
UPDATE roles
SET permissions = permissions | 1024
WHERE is_default = TRUE
  AND deleted_at = 0;
