CREATE TABLE IF NOT EXISTS user_credentials (
	user_id BIGINT PRIMARY KEY,
	hashed_password TEXT NOT NULL,
	created_at BIGINT NOT NULL,
	updated_at BIGINT NOT NULL DEFAULT 0
);
