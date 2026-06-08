CREATE TABLE IF NOT EXISTS sessions (
	session_id BIGINT PRIMARY KEY,
	user_id BIGINT NOT NULL,
	refresh_token_hash TEXT NOT NULL UNIQUE,
	user_agent TEXT NOT NULL DEFAULT '',
	ip TEXT NOT NULL DEFAULT '',
	created_at BIGINT NOT NULL,
	updated_at BIGINT NOT NULL DEFAULT 0,
	expires_at BIGINT NOT NULL,
	revoked_at BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS sessions_user_id_idx
	ON sessions (user_id)
	WHERE revoked_at = 0;
