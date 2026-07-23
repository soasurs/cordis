CREATE TABLE IF NOT EXISTS assets (
    id                      BIGINT PRIMARY KEY CHECK (id > 0),
    created_by_user_id      BIGINT NOT NULL CHECK (created_by_user_id > 0),
    subject_id              BIGINT NOT NULL CHECK (subject_id > 0),
    kind                    TEXT NOT NULL CHECK (kind IN ('user_avatar','guild_icon','message_attachment')),
    status                  TEXT NOT NULL DEFAULT 'CREATED' CHECK (status IN ('CREATED','COMPLETING','UPLOADED','PROCESSING','READY','FAILED','ABORTED','EXPIRED')),
    storage_backend         TEXT NOT NULL DEFAULT '',
    staging_key             TEXT NOT NULL DEFAULT '',
    published_key           TEXT NOT NULL DEFAULT '',
    expected_size           BIGINT NOT NULL DEFAULT 0 CHECK (expected_size >= 0),
    actual_size             BIGINT NOT NULL DEFAULT 0 CHECK (actual_size >= 0),
    content_type            TEXT NOT NULL DEFAULT '',
    expires_at              BIGINT NOT NULL DEFAULT 0,
    width                   INT NOT NULL DEFAULT 0 CHECK (width >= 0),
    height                  INT NOT NULL DEFAULT 0 CHECK (height >= 0),
    variants                JSONB NOT NULL DEFAULT '[]'::jsonb,
    error_message           TEXT NOT NULL DEFAULT '',
    created_at              BIGINT NOT NULL DEFAULT 0,
    updated_at              BIGINT NOT NULL DEFAULT 0,
    deleted_at              BIGINT NOT NULL DEFAULT 0 CHECK (deleted_at >= 0)
);

CREATE INDEX IF NOT EXISTS assets_created_by_user_id_status_idx
    ON assets (created_by_user_id, status)
    WHERE deleted_at = 0;

CREATE INDEX IF NOT EXISTS assets_status_expires_idx
    ON assets (status, expires_at)
    WHERE status = 'CREATED' AND expires_at > 0 AND deleted_at = 0;
