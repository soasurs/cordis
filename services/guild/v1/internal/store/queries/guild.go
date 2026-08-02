package queries

const CreateGuildQuery = `
    INSERT INTO guilds (
        id, owner_id, name, description, icon_asset_id, revision, created_at, updated_at, deleted_at
    ) VALUES (
        $1, $2, $3, '', 0, 1, $4, 0, 0
    )
    RETURNING ` + GuildColumns

const CreateDefaultRoleStatement = `
    INSERT INTO roles (
        id, guild_id, name, permissions, position, is_default,
        revision, created_at, updated_at, deleted_at
    ) VALUES (
        $1, $1, '@everyone', 1120, 0, TRUE, 1, $2, 0, 0
    )
`

const GetGuildForMemberQuery = `
    SELECT ` + GuildColumns + `
    FROM guilds
    WHERE id = $1
      AND deleted_at = 0
      AND EXISTS (
          SELECT 1
          FROM guild_members
          WHERE guild_id = guilds.id
            AND user_id = $2
            AND deleted_at = 0
      )
    LIMIT 1
`

const ListUserGuildsQuery = `
    SELECT ` + GuildColumns + `
    FROM guilds
    WHERE deleted_at = 0
      AND ($2::BIGINT = 0 OR id < $2::BIGINT)
      AND EXISTS (
          SELECT 1
          FROM guild_members
          WHERE guild_id = guilds.id
            AND user_id = $1
            AND deleted_at = 0
      )
    ORDER BY id DESC
    LIMIT $3
`

const UpdateGuildQuery = `
    UPDATE guilds
    SET name = CASE WHEN $2 THEN $3 ELSE name END,
        description = CASE WHEN $4 THEN $5 ELSE description END,
        revision = revision + 1,
        updated_at = $6
    WHERE id = $1
      AND deleted_at = 0
    RETURNING ` + GuildColumns

const UpdateGuildIconQuery = `
    UPDATE guilds
    SET icon_asset_id = $2,
        revision = revision + 1,
        updated_at = $3
    WHERE id = $1
      AND deleted_at = 0
    RETURNING ` + GuildColumns

const DeleteGuildQuery = `
    UPDATE guilds
    SET revision = revision + 1,
        updated_at = $2,
        deleted_at = $2
    WHERE id = $1
      AND deleted_at = 0
    RETURNING ` + GuildColumns

const DeleteGuildMembersStatement = `
    UPDATE guild_members
    SET revision = revision + 1,
        updated_at = $2,
        deleted_at = $2
    WHERE guild_id = $1
      AND deleted_at = 0
`

const DeleteGuildRolesStatement = `
    UPDATE roles
    SET revision = revision + 1,
        updated_at = $2,
        deleted_at = $2
    WHERE guild_id = $1
      AND deleted_at = 0
`

const GetGuildQuery = `
    SELECT ` + GuildColumns + `
    FROM guilds
    WHERE id = $1
      AND deleted_at = 0
    LIMIT 1
`

const GetGuildChannelLayoutRevisionQuery = `
    SELECT channel_layout_revision
    FROM guilds
    WHERE id = $1
      AND deleted_at = 0
    LIMIT 1
`

const AdvanceGuildChannelLayoutRevisionQuery = `
    UPDATE guilds
    SET channel_layout_revision = channel_layout_revision + 1
    WHERE id = $1
      AND deleted_at = 0
      AND channel_layout_revision = $2
    RETURNING channel_layout_revision
`

const CountGuildMembersQuery = `
    SELECT COUNT(*)
    FROM guild_members
    WHERE guild_id = $1
      AND deleted_at = 0
`

const TransferGuildOwnershipQuery = `
    UPDATE guilds
    SET owner_id = $3,
        revision = revision + 1,
        updated_at = $4
    WHERE id = $1
      AND owner_id = $2
      AND deleted_at = 0
    RETURNING ` + GuildColumns
