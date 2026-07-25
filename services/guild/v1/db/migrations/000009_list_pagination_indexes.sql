-- Align list pagination indexes with keyset orderings.

CREATE INDEX IF NOT EXISTS guild_members_guild_joined_idx
    ON guild_members (guild_id, joined_at DESC, user_id DESC)
    WHERE deleted_at = 0;

DROP INDEX IF EXISTS guild_bans_guild_user_idx;

CREATE INDEX IF NOT EXISTS guild_bans_guild_created_idx
    ON guild_bans (guild_id, created_at DESC, user_id DESC);
