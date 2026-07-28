ALTER TABLE sessions
	ADD COLUMN absolute_expires_at BIGINT NOT NULL DEFAULT 0,
	ADD COLUMN refresh_token_id TEXT NOT NULL DEFAULT '',
	ADD COLUMN refresh_token_issued_at BIGINT NOT NULL DEFAULT 0,
	ADD COLUMN refresh_token_expires_at BIGINT NOT NULL DEFAULT 0,
	ADD COLUMN previous_refresh_token_hash TEXT NOT NULL DEFAULT '',
	ADD COLUMN previous_refresh_token_valid_until BIGINT NOT NULL DEFAULT 0;

UPDATE sessions
SET absolute_expires_at = expires_at
WHERE absolute_expires_at = 0;
