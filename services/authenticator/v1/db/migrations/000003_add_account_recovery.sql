CREATE TABLE IF NOT EXISTS password_reset_tokens (
	user_id BIGINT PRIMARY KEY,
	token_hash TEXT NOT NULL UNIQUE,
	created_at BIGINT NOT NULL,
	expires_at BIGINT NOT NULL,
	consumed_at BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS email_verification_tokens (
	user_id BIGINT PRIMARY KEY,
	token_hash TEXT NOT NULL UNIQUE,
	email TEXT NOT NULL,
	created_at BIGINT NOT NULL,
	expires_at BIGINT NOT NULL,
	consumed_at BIGINT NOT NULL DEFAULT 0
);
