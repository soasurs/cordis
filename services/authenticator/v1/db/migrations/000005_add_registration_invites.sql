CREATE TABLE IF NOT EXISTS registration_invites (
	id                  BIGINT PRIMARY KEY CHECK (id > 0),
	code_hash           TEXT NOT NULL,
	bound_email         TEXT NOT NULL DEFAULT '',
	reserved_email      TEXT NOT NULL DEFAULT '',
	reserved_until      BIGINT NOT NULL DEFAULT 0 CHECK (reserved_until >= 0),
	redeemed_user_id    BIGINT NOT NULL DEFAULT 0 CHECK (redeemed_user_id >= 0),
	redeemed_at         BIGINT NOT NULL DEFAULT 0 CHECK (redeemed_at >= 0),
	expires_at          BIGINT NOT NULL DEFAULT 0 CHECK (expires_at >= 0),
	revoked_at          BIGINT NOT NULL DEFAULT 0 CHECK (revoked_at >= 0),
	label               TEXT NOT NULL DEFAULT '',
	created_at          BIGINT NOT NULL CHECK (created_at > 0),
	CHECK ((redeemed_user_id = 0) = (redeemed_at = 0))
);

CREATE UNIQUE INDEX IF NOT EXISTS registration_invites_code_hash_idx
	ON registration_invites (code_hash);

CREATE UNIQUE INDEX IF NOT EXISTS registration_invites_redeemed_user_idx
	ON registration_invites (redeemed_user_id)
	WHERE redeemed_user_id > 0;

CREATE INDEX IF NOT EXISTS registration_invites_created_idx
	ON registration_invites (id DESC);
