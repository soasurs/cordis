-- Passwords moved to the authenticator service (user_credentials); this
-- deployment has no production data, so the column is dropped rather than
-- migrated.
ALTER TABLE users
    DROP COLUMN IF EXISTS hashed_password;
