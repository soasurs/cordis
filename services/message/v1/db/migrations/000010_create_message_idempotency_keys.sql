CREATE TABLE IF NOT EXISTS message_idempotency_keys (
    actor_user_id   BIGINT NOT NULL CHECK (actor_user_id > 0),
    operation       TEXT NOT NULL CHECK (operation <> ''),
    idempotency_key TEXT NOT NULL CHECK (idempotency_key <> ''),
    request_hash    BYTEA NOT NULL CHECK (octet_length(request_hash) = 32),
    message_id      BIGINT NOT NULL CHECK (message_id > 0),
    created_at      BIGINT NOT NULL CHECK (created_at > 0),
    expires_at      BIGINT NOT NULL CHECK (expires_at > created_at),
    PRIMARY KEY (actor_user_id, operation, idempotency_key)
);

CREATE INDEX IF NOT EXISTS message_idempotency_keys_expires_at_idx
    ON message_idempotency_keys (expires_at);
