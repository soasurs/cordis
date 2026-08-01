ALTER TABLE guild_member_profiles
    ADD COLUMN IF NOT EXISTS nickname TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS nickname_search TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS guild_member_profiles_nickname_search_idx
    ON guild_member_profiles (guild_id, nickname_search text_pattern_ops, user_id);

CREATE INDEX IF NOT EXISTS guild_member_profiles_user_id_idx
    ON guild_member_profiles (user_id);
