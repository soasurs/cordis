-- Direct <@user> mentions are written synchronously with source 1. Rows for
-- role and @everyone mentions are expanded asynchronously with source 2 so
-- the (message_id, user_id) primary key keeps one row per user per message
-- regardless of how many mention sources matched.

ALTER TABLE message_mentions
    ADD COLUMN source SMALLINT NOT NULL DEFAULT 1
    CHECK (source IN (1, 2));

CREATE TABLE IF NOT EXISTS message_role_mentions (
    message_id  BIGINT NOT NULL,
    role_id     BIGINT NOT NULL CHECK (role_id > 0),
    PRIMARY KEY (message_id, role_id)
);

ALTER TABLE messages
    ADD COLUMN mention_everyone BOOLEAN NOT NULL DEFAULT FALSE;
