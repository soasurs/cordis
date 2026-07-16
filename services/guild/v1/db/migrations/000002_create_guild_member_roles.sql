CREATE TABLE IF NOT EXISTS guild_member_roles (
    guild_id    BIGINT NOT NULL CHECK (guild_id > 0),
    user_id     BIGINT NOT NULL CHECK (user_id > 0),
    role_id     BIGINT NOT NULL CHECK (role_id > 0),
    created_at  BIGINT NOT NULL CHECK (created_at > 0),
    PRIMARY KEY (guild_id, user_id, role_id)
);

CREATE INDEX IF NOT EXISTS guild_member_roles_role_idx
    ON guild_member_roles (guild_id, role_id, user_id);
