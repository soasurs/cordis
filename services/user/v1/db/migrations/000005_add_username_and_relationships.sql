-- Globally unique lowercase handle. Existing rows get a deterministic
-- placeholder derived from the user ID; real users pick their handle at
-- registration from this migration on.
ALTER TABLE user_profiles
    ADD COLUMN IF NOT EXISTS username TEXT NOT NULL DEFAULT '';

UPDATE user_profiles
SET username = 'user_' || user_id::text
WHERE username = '';

ALTER TABLE user_profiles
    ALTER COLUMN username DROP DEFAULT;

CREATE UNIQUE INDEX IF NOT EXISTS user_profiles_username_active_idx
    ON user_profiles (username)
    WHERE deleted_at = 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'user_profiles_username_format_check'
    ) THEN
        ALTER TABLE user_profiles
            ADD CONSTRAINT user_profiles_username_format_check
            CHECK (username ~ '^[a-z0-9_]{2,32}$');
    END IF;
END $$;

-- One row per direction: (user_id, target_id) is how user_id sees target_id.
-- Types: 1 outgoing request, 2 incoming request, 3 friend, 4 blocked.
-- Mutations maintain both directions inside one transaction.
CREATE TABLE IF NOT EXISTS user_relationships (
    user_id     BIGINT NOT NULL CHECK (user_id > 0),
    target_id   BIGINT NOT NULL CHECK (target_id > 0),
    type        SMALLINT NOT NULL CHECK (type BETWEEN 1 AND 4),
    created_at  BIGINT NOT NULL CHECK (created_at > 0),
    updated_at  BIGINT NOT NULL DEFAULT 0 CHECK (updated_at >= 0),
    PRIMARY KEY (user_id, target_id),
    CHECK (user_id <> target_id)
);

CREATE INDEX IF NOT EXISTS user_relationships_user_type_idx
    ON user_relationships (user_id, type, target_id DESC);
