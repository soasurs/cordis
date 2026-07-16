package store

const guildColumns = `
    id, owner_id, name, icon_uri, revision, created_at, updated_at, deleted_at
`

const guildMemberColumns = `
    guild_id, user_id, nickname, revision, joined_at, updated_at, deleted_at
`

const createGuildQuery = `
    INSERT INTO guilds (
        id, owner_id, name, icon_uri, revision, created_at, updated_at, deleted_at
    ) VALUES (
        $1, $2, $3, $4, 1, $5, 0, 0
    )
    RETURNING ` + guildColumns

const createGuildMemberQuery = `
    INSERT INTO guild_members (
        guild_id, user_id, nickname, revision, joined_at, updated_at, deleted_at
    ) VALUES (
        $1, $2, '', 1, $3, 0, 0
    )
    ON CONFLICT (guild_id, user_id) DO UPDATE
    SET nickname = '',
        revision = guild_members.revision + 1,
        joined_at = EXCLUDED.joined_at,
        updated_at = EXCLUDED.joined_at,
        deleted_at = 0
    WHERE guild_members.deleted_at <> 0
    RETURNING ` + guildMemberColumns

const createDefaultRoleStatement = `
    INSERT INTO roles (
        id, guild_id, name, permissions, position, is_default,
        revision, created_at, updated_at, deleted_at
    ) VALUES (
        $1, $1, '@everyone', 0, 0, TRUE, 1, $2, 0, 0
    )
`

const getGuildForMemberQuery = `
    SELECT ` + guildColumns + `
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

const listUserGuildsQuery = `
    SELECT ` + guildColumns + `
    FROM guilds
    WHERE deleted_at = 0
      AND ($2 = 0 OR id < $2)
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

const updateGuildQuery = `
    UPDATE guilds
    SET name = CASE WHEN $2 THEN $3 ELSE name END,
        icon_uri = CASE WHEN $4 THEN $5 ELSE icon_uri END,
        revision = revision + 1,
        updated_at = $6
    WHERE id = $1
      AND deleted_at = 0
    RETURNING ` + guildColumns

const deleteGuildQuery = `
    UPDATE guilds
    SET revision = revision + 1,
        updated_at = $2,
        deleted_at = $2
    WHERE id = $1
      AND deleted_at = 0
    RETURNING ` + guildColumns

const deleteGuildMembersStatement = `
    UPDATE guild_members
    SET revision = revision + 1,
        updated_at = $2,
        deleted_at = $2
    WHERE guild_id = $1
      AND deleted_at = 0
`

const deleteGuildRolesStatement = `
    UPDATE roles
    SET revision = revision + 1,
        updated_at = $2,
        deleted_at = $2
    WHERE guild_id = $1
      AND deleted_at = 0
`

const getGuildMemberQuery = `
    SELECT ` + guildMemberColumns + `
    FROM guild_members
    WHERE guild_id = $1
      AND user_id = $2
      AND deleted_at = 0
    LIMIT 1
`

const listGuildMembersQuery = `
    SELECT ` + guildMemberColumns + `
    FROM guild_members
    WHERE guild_id = $1
      AND deleted_at = 0
      AND ($2 = 0 OR user_id < $2)
    ORDER BY user_id DESC
    LIMIT $3
`

const updateGuildMemberNicknameQuery = `
    UPDATE guild_members
    SET nickname = $3,
        revision = revision + 1,
        updated_at = $4
    WHERE guild_id = $1
      AND user_id = $2
      AND deleted_at = 0
    RETURNING ` + guildMemberColumns

const removeGuildMemberQuery = `
    UPDATE guild_members
    SET revision = revision + 1,
        updated_at = $3,
        deleted_at = $3
    WHERE guild_id = $1
      AND user_id = $2
      AND deleted_at = 0
    RETURNING ` + guildMemberColumns

const transferGuildOwnershipQuery = `
    UPDATE guilds
    SET owner_id = $3,
        revision = revision + 1,
        updated_at = $4
    WHERE id = $1
      AND owner_id = $2
      AND deleted_at = 0
    RETURNING ` + guildColumns
