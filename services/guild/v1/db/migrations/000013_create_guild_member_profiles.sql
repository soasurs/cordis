CREATE TABLE IF NOT EXISTS guild_member_profiles (
    guild_id           BIGINT NOT NULL CHECK (guild_id > 0),
    user_id            BIGINT NOT NULL CHECK (user_id > 0),
    username           TEXT NOT NULL,
    name               TEXT NOT NULL DEFAULT '',
    username_search    TEXT NOT NULL,
    name_search        TEXT NOT NULL DEFAULT '',
    avatar_asset_id    BIGINT NOT NULL DEFAULT 0 CHECK (avatar_asset_id >= 0),
    profile_updated_at BIGINT NOT NULL DEFAULT 0 CHECK (profile_updated_at >= 0),
    PRIMARY KEY (guild_id, user_id)
);

CREATE INDEX IF NOT EXISTS guild_member_profiles_username_search_idx
    ON guild_member_profiles (guild_id, username_search text_pattern_ops, user_id);

CREATE INDEX IF NOT EXISTS guild_member_profiles_name_search_idx
    ON guild_member_profiles (guild_id, name_search text_pattern_ops, user_id);
