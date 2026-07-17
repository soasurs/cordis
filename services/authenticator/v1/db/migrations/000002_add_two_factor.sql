CREATE TABLE IF NOT EXISTS user_totp_factors (
	user_id BIGINT PRIMARY KEY,
	secret_ciphertext BYTEA NOT NULL,
	encryption_key_id TEXT NOT NULL,
	last_used_counter BIGINT NOT NULL DEFAULT -1,
	enabled_at BIGINT NOT NULL,
	created_at BIGINT NOT NULL,
	updated_at BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS totp_enrollments (
	user_id BIGINT PRIMARY KEY,
	token_hash TEXT NOT NULL UNIQUE,
	secret_ciphertext BYTEA NOT NULL,
	encryption_key_id TEXT NOT NULL,
	created_at BIGINT NOT NULL,
	expires_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS two_factor_login_challenges (
	token_hash TEXT PRIMARY KEY,
	user_id BIGINT NOT NULL,
	user_agent TEXT NOT NULL DEFAULT '',
	ip TEXT NOT NULL DEFAULT '',
	attempts INT NOT NULL DEFAULT 0,
	created_at BIGINT NOT NULL,
	expires_at BIGINT NOT NULL,
	consumed_at BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS two_factor_login_challenges_user_id_idx
	ON two_factor_login_challenges (user_id, expires_at)
	WHERE consumed_at = 0;

CREATE TABLE IF NOT EXISTS two_factor_recovery_codes (
	user_id BIGINT NOT NULL,
	code_hash TEXT NOT NULL,
	created_at BIGINT NOT NULL,
	used_at BIGINT NOT NULL DEFAULT 0,
	PRIMARY KEY (user_id, code_hash)
);
