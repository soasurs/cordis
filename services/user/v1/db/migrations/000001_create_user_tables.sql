CREATE TABLE IF NOT EXISTS users (
	user_id BIGINT PRIMARY KEY,
	email TEXT NOT NULL,
	hashed_password TEXT NOT NULL,
	created_at BIGINT NOT NULL,
	updated_at BIGINT NOT NULL DEFAULT 0,
	deleted_at BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS users_email_active_idx
	ON users (email)
	WHERE deleted_at = 0;

CREATE TABLE IF NOT EXISTS user_profiles (
	user_id BIGINT PRIMARY KEY,
	name TEXT NOT NULL DEFAULT '',
	avatar_asset_id BIGINT NOT NULL DEFAULT 0 CHECK (avatar_asset_id >= 0),
	created_at BIGINT NOT NULL,
	updated_at BIGINT NOT NULL DEFAULT 0,
	deleted_at BIGINT NOT NULL DEFAULT 0
);
