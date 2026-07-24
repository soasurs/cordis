ALTER TABLE guilds
    ADD COLUMN IF NOT EXISTS access_revision BIGINT NOT NULL DEFAULT 1
    CHECK (access_revision > 0);

CREATE OR REPLACE FUNCTION bump_guild_access_revision()
RETURNS TRIGGER AS $$
DECLARE
    affected_guild_id BIGINT;
BEGIN
    affected_guild_id := CASE WHEN TG_OP = 'DELETE' THEN OLD.guild_id ELSE NEW.guild_id END;
    UPDATE guilds
    SET access_revision = access_revision + 1
    WHERE id = affected_guild_id;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION bump_owned_guild_access_revision()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE guilds
    SET access_revision = access_revision + 1
    WHERE id = NEW.id;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS guild_members_access_revision ON guild_members;
DROP TRIGGER IF EXISTS roles_access_revision ON roles;
DROP TRIGGER IF EXISTS guild_member_roles_access_revision ON guild_member_roles;
DROP TRIGGER IF EXISTS guild_channels_access_revision ON guild_channels;
DROP TRIGGER IF EXISTS guild_channel_overwrites_access_revision ON guild_channel_permission_overwrites;
DROP TRIGGER IF EXISTS guild_owner_access_revision ON guilds;

CREATE TRIGGER guild_members_access_revision
AFTER INSERT OR DELETE OR UPDATE OF deleted_at ON guild_members
FOR EACH ROW EXECUTE FUNCTION bump_guild_access_revision();

CREATE TRIGGER roles_access_revision
AFTER INSERT OR DELETE OR UPDATE OF permissions, deleted_at ON roles
FOR EACH ROW EXECUTE FUNCTION bump_guild_access_revision();

CREATE TRIGGER guild_member_roles_access_revision
AFTER INSERT OR UPDATE OR DELETE ON guild_member_roles
FOR EACH ROW EXECUTE FUNCTION bump_guild_access_revision();

CREATE TRIGGER guild_channels_access_revision
AFTER INSERT OR DELETE OR UPDATE OF deleted_at ON guild_channels
FOR EACH ROW EXECUTE FUNCTION bump_guild_access_revision();

CREATE TRIGGER guild_channel_overwrites_access_revision
AFTER INSERT OR UPDATE OR DELETE ON guild_channel_permission_overwrites
FOR EACH ROW EXECUTE FUNCTION bump_guild_access_revision();

CREATE TRIGGER guild_owner_access_revision
AFTER UPDATE OF owner_id, deleted_at ON guilds
FOR EACH ROW
WHEN (OLD.owner_id IS DISTINCT FROM NEW.owner_id OR OLD.deleted_at IS DISTINCT FROM NEW.deleted_at)
EXECUTE FUNCTION bump_owned_guild_access_revision();
