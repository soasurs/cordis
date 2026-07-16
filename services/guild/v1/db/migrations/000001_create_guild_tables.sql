CREATE TABLE IF NOT EXISTS guilds (
    id          BIGINT PRIMARY KEY CHECK (id > 0),
    owner_id    BIGINT NOT NULL CHECK (owner_id > 0),
    name        TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    icon_uri    TEXT NOT NULL DEFAULT '',
    revision    BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at  BIGINT NOT NULL CHECK (created_at > 0),
    updated_at  BIGINT NOT NULL DEFAULT 0 CHECK (updated_at >= 0),
    deleted_at  BIGINT NOT NULL DEFAULT 0 CHECK (deleted_at >= 0)
);

CREATE INDEX IF NOT EXISTS guilds_owner_id_idx
    ON guilds (owner_id, id DESC)
    WHERE deleted_at = 0;

CREATE TABLE IF NOT EXISTS guild_members (
    guild_id    BIGINT NOT NULL CHECK (guild_id > 0),
    user_id     BIGINT NOT NULL CHECK (user_id > 0),
    nickname    TEXT NOT NULL DEFAULT '',
    revision    BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    joined_at   BIGINT NOT NULL CHECK (joined_at > 0),
    updated_at  BIGINT NOT NULL DEFAULT 0 CHECK (updated_at >= 0),
    deleted_at  BIGINT NOT NULL DEFAULT 0 CHECK (deleted_at >= 0),
    PRIMARY KEY (guild_id, user_id)
);

CREATE INDEX IF NOT EXISTS guild_members_user_guild_idx
    ON guild_members (user_id, guild_id DESC)
    WHERE deleted_at = 0;

CREATE TABLE IF NOT EXISTS roles (
    id           BIGINT PRIMARY KEY CHECK (id > 0),
    guild_id     BIGINT NOT NULL CHECK (guild_id > 0),
    name         TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    permissions  BIGINT NOT NULL DEFAULT 0 CHECK (permissions >= 0),
    position     INT NOT NULL DEFAULT 0 CHECK (position >= 0),
    is_default   BOOLEAN NOT NULL DEFAULT FALSE,
    revision     BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at   BIGINT NOT NULL CHECK (created_at > 0),
    updated_at   BIGINT NOT NULL DEFAULT 0 CHECK (updated_at >= 0),
    deleted_at   BIGINT NOT NULL DEFAULT 0 CHECK (deleted_at >= 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS roles_guild_default_active_idx
    ON roles (guild_id)
    WHERE is_default AND deleted_at = 0;

CREATE INDEX IF NOT EXISTS roles_guild_position_idx
    ON roles (guild_id, position, id)
    WHERE deleted_at = 0;
