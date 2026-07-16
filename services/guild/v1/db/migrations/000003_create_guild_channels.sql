CREATE TABLE IF NOT EXISTS guild_channels (
    id          BIGINT PRIMARY KEY CHECK (id > 0),
    guild_id    BIGINT NOT NULL CHECK (guild_id > 0),
    name        TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    type        INT NOT NULL CHECK (type = 1),
    position    INT NOT NULL CHECK (position >= 0),
    topic       TEXT NOT NULL DEFAULT '' CHECK (char_length(topic) <= 1024),
    revision    BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at  BIGINT NOT NULL CHECK (created_at > 0),
    updated_at  BIGINT NOT NULL DEFAULT 0 CHECK (updated_at >= 0),
    deleted_at  BIGINT NOT NULL DEFAULT 0 CHECK (deleted_at >= 0)
);

CREATE INDEX IF NOT EXISTS guild_channels_guild_position_idx
    ON guild_channels (guild_id, position, id)
    WHERE deleted_at = 0;

CREATE TABLE IF NOT EXISTS guild_channel_permission_overwrites (
    channel_id   BIGINT NOT NULL CHECK (channel_id > 0),
    guild_id     BIGINT NOT NULL CHECK (guild_id > 0),
    target_type  INT NOT NULL CHECK (target_type IN (1, 2)),
    target_id    BIGINT NOT NULL CHECK (target_id > 0),
    allow_bits   BIGINT NOT NULL DEFAULT 0 CHECK (allow_bits >= 0),
    deny_bits    BIGINT NOT NULL DEFAULT 0 CHECK (deny_bits >= 0),
    revision     BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at   BIGINT NOT NULL CHECK (created_at > 0),
    updated_at   BIGINT NOT NULL DEFAULT 0 CHECK (updated_at >= 0),
    PRIMARY KEY (channel_id, target_type, target_id),
    CHECK ((allow_bits & deny_bits) = 0)
);

CREATE INDEX IF NOT EXISTS guild_channel_overwrites_guild_idx
    ON guild_channel_permission_overwrites (guild_id, channel_id, target_type, target_id);

UPDATE roles
SET permissions = permissions | 96,
    revision = revision + 1
WHERE is_default
  AND deleted_at = 0
  AND (permissions & 96) <> 96;
