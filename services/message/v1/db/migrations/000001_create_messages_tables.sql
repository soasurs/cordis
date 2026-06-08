CREATE TABLE IF NOT EXISTS messages (
    id              BIGINT PRIMARY KEY CHECK (id > 0),
    channel_id      BIGINT NOT NULL CHECK (channel_id > 0),
    author_id       BIGINT NOT NULL CHECK (author_id > 0),
    content         TEXT NOT NULL DEFAULT '',

    type            SMALLINT NOT NULL DEFAULT 1,
    flags           INT NOT NULL DEFAULT 0 CHECK (flags >= 0),

    referenced_message_id   BIGINT,
    referenced_channel_id   BIGINT,

    attachments     JSONB NOT NULL DEFAULT '[]'::jsonb,

    edited_at       BIGINT,

    created_at      BIGINT NOT NULL CHECK (created_at > 0),
    updated_at      BIGINT NOT NULL DEFAULT 0 CHECK (updated_at >= 0),
    deleted_at      BIGINT NOT NULL DEFAULT 0 CHECK (deleted_at >= 0),

    CHECK (type IN (1, 19, 21)),
    CHECK (jsonb_typeof(attachments) = 'array'),
    CHECK (content <> '' OR jsonb_array_length(attachments) > 0),
    CHECK (
        (referenced_message_id IS NULL AND referenced_channel_id IS NULL)
        OR (referenced_message_id IS NOT NULL AND referenced_channel_id IS NOT NULL)
    ),
    CHECK (type <> 19 OR referenced_message_id IS NOT NULL),
);

CREATE INDEX messages_channel_id_id_desc_idx
    ON messages (channel_id, id DESC)
    WHERE deleted_at = 0;

CREATE INDEX messages_referenced_idx
    ON messages (referenced_message_id)
    WHERE referenced_message_id IS NOT NULL AND deleted_at = 0;

CREATE TABLE IF NOT EXISTS message_mentions (
    message_id  BIGINT NOT NULL,
    user_id     BIGINT NOT NULL CHECK (user_id > 0),
    PRIMARY KEY (message_id, user_id),
);

CREATE INDEX message_mentions_user_idx
    ON message_mentions (user_id, message_id DESC);

CREATE TABLE IF NOT EXISTS reactions (
    message_id  BIGINT NOT NULL,
    user_id     BIGINT NOT NULL CHECK (user_id > 0),
    emoji_id    BIGINT NOT NULL DEFAULT 0 CHECK (emoji_id >= 0),
    emoji_name  TEXT NOT NULL CHECK (emoji_name <> ''),
    created_at  BIGINT NOT NULL CHECK (created_at > 0),
    PRIMARY KEY (message_id, user_id, emoji_id, emoji_name),
);

CREATE INDEX reactions_message_emoji_user_idx
    ON reactions (message_id, emoji_id, emoji_name, user_id);

-- Only custom guild emojis are stored here; Unicode emojis have id=0
-- in reactions and are handled by the client.
CREATE TABLE IF NOT EXISTS emojis (
    id          BIGINT PRIMARY KEY CHECK (id > 0),
    guild_id    BIGINT NOT NULL CHECK (guild_id > 0),
    name        TEXT NOT NULL CHECK (name <> ''),
    image_key   TEXT NOT NULL CHECK (image_key <> ''),
    animated    BOOLEAN NOT NULL DEFAULT FALSE,
    created_by  BIGINT NOT NULL CHECK (created_by > 0),
    created_at  BIGINT NOT NULL CHECK (created_at > 0)
);

CREATE INDEX emojis_guild_name_idx
    ON emojis (guild_id, name);
