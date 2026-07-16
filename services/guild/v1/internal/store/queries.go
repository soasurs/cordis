package store

const guildColumns = `
    id, owner_id, name, icon_uri, revision, created_at, updated_at, deleted_at
`

const guildMemberColumns = `
    guild_id, user_id, nickname, revision, joined_at, updated_at, deleted_at
`

const guildBanColumns = `
    guild_id, user_id, actor_user_id, reason, created_at
`

const roleColumns = `
    id, guild_id, name, permissions, position, is_default,
    revision, created_at, updated_at, deleted_at
`

const channelColumns = `
    id, guild_id, name, type, position, topic, revision, created_at, updated_at, deleted_at, parent_id
`

const channelOverwriteColumns = `
    channel_id, guild_id, target_type, target_id, allow_bits, deny_bits,
    revision, created_at, updated_at
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
    )
    SELECT $1, $2, '', 1, $3, 0, 0
    WHERE NOT EXISTS (
        SELECT 1 FROM guild_bans
        WHERE guild_id = $1 AND user_id = $2
    )
    ON CONFLICT (guild_id, user_id) DO UPDATE
    SET nickname = '',
        revision = guild_members.revision + 1,
        joined_at = EXCLUDED.joined_at,
        updated_at = EXCLUDED.joined_at,
        deleted_at = 0
    WHERE guild_members.deleted_at <> 0
      AND NOT EXISTS (
          SELECT 1 FROM guild_bans
          WHERE guild_id = $1 AND user_id = $2
      )
    RETURNING ` + guildMemberColumns

const createDefaultRoleStatement = `
    INSERT INTO roles (
        id, guild_id, name, permissions, position, is_default,
        revision, created_at, updated_at, deleted_at
    ) VALUES (
        $1, $1, '@everyone', 96, 0, TRUE, 1, $2, 0, 0
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

const upsertGuildBanQuery = `
    INSERT INTO guild_bans (guild_id, user_id, actor_user_id, reason, created_at)
    VALUES ($1, $2, $3, $4, $5)
    ON CONFLICT (guild_id, user_id) DO UPDATE
    SET actor_user_id = EXCLUDED.actor_user_id,
        reason = EXCLUDED.reason,
        created_at = EXCLUDED.created_at
    RETURNING ` + guildBanColumns

const deleteGuildBanStatement = `
    DELETE FROM guild_bans
    WHERE guild_id = $1
      AND user_id = $2
`

const getGuildBanQuery = `
    SELECT ` + guildBanColumns + `
    FROM guild_bans
    WHERE guild_id = $1
      AND user_id = $2
    LIMIT 1
`

const listGuildBansQuery = `
    SELECT ` + guildBanColumns + `
    FROM guild_bans
    WHERE guild_id = $1
      AND ($2 = 0 OR user_id < $2)
    ORDER BY user_id DESC
    LIMIT $3
`

const deleteGuildBansStatement = `
    DELETE FROM guild_bans
    WHERE guild_id = $1
`

const transferGuildOwnershipQuery = `
    UPDATE guilds
    SET owner_id = $3,
        revision = revision + 1,
        updated_at = $4
    WHERE id = $1
      AND owner_id = $2
      AND deleted_at = 0
    RETURNING ` + guildColumns

const createGuildRoleQuery = `
    INSERT INTO roles (
        id, guild_id, name, permissions, position, is_default,
        revision, created_at, updated_at, deleted_at
    ) VALUES (
        $1, $2, $3, $4, $5, FALSE, 1, $6, 0, 0
    )
    RETURNING ` + roleColumns

const getGuildRoleQuery = `
    SELECT ` + roleColumns + `
    FROM roles
    WHERE guild_id = $1
      AND id = $2
      AND deleted_at = 0
    LIMIT 1
`

const listGuildRolesQuery = `
    SELECT ` + roleColumns + `
    FROM roles
    WHERE guild_id = $1
      AND deleted_at = 0
    ORDER BY position DESC, id ASC
`

const updateGuildRoleQuery = `
    UPDATE roles
    SET name = CASE WHEN $3 THEN $4 ELSE name END,
        permissions = CASE WHEN $5 THEN $6 ELSE permissions END,
        revision = revision + 1,
        updated_at = $7
    WHERE guild_id = $1
      AND id = $2
      AND deleted_at = 0
    RETURNING ` + roleColumns

const updateGuildRolePositionQuery = `
    UPDATE roles
    SET position = $3,
        revision = revision + 1,
        updated_at = $4
    WHERE guild_id = $1
      AND id = $2
      AND is_default = FALSE
      AND deleted_at = 0
    RETURNING ` + roleColumns

const deleteGuildRoleQuery = `
    UPDATE roles
    SET revision = revision + 1,
        updated_at = $3,
        deleted_at = $3
    WHERE guild_id = $1
      AND id = $2
      AND is_default = FALSE
      AND deleted_at = 0
    RETURNING ` + roleColumns

const addGuildMemberRoleStatement = `
    INSERT INTO guild_member_roles (guild_id, user_id, role_id, created_at)
    VALUES ($1, $2, $3, $4)
    ON CONFLICT (guild_id, user_id, role_id) DO NOTHING
`

const removeGuildMemberRoleStatement = `
    DELETE FROM guild_member_roles
    WHERE guild_id = $1
      AND user_id = $2
      AND role_id = $3
`

const deleteGuildMemberRoleAssignmentsStatement = `
    DELETE FROM guild_member_roles
    WHERE guild_id = $1
      AND user_id = $2
`

const deleteGuildRoleAssignmentsStatement = `
    DELETE FROM guild_member_roles
    WHERE guild_id = $1
      AND role_id = $2
`

const deleteAllGuildRoleAssignmentsStatement = `
    DELETE FROM guild_member_roles
    WHERE guild_id = $1
`

const listGuildMemberRolesQuery = `
    SELECT ` + roleColumns + `
    FROM roles
    WHERE guild_id = $1
      AND deleted_at = 0
      AND (
          is_default = TRUE
          OR id IN (
              SELECT role_id
              FROM guild_member_roles
              WHERE guild_id = $1
                AND user_id = $2
          )
      )
    ORDER BY position DESC, id ASC
`

const createGuildChannelQuery = `
    INSERT INTO guild_channels (
        id, guild_id, name, type, position, topic, revision, created_at, updated_at, deleted_at, parent_id
    ) VALUES ($1, $2, $3, $4, $5, $6, 1, $8, 0, 0, $7)
    RETURNING ` + channelColumns

const getGuildChannelQuery = `
    SELECT ` + channelColumns + `
    FROM guild_channels
    WHERE id = $1
      AND deleted_at = 0
    LIMIT 1
`

const listGuildChannelsQuery = `
    SELECT ` + channelColumns + `
    FROM guild_channels
    WHERE guild_id = $1
      AND deleted_at = 0
    ORDER BY position ASC, id ASC
`

const updateGuildChannelQuery = `
    UPDATE guild_channels
    SET name = CASE WHEN $2 THEN $3 ELSE name END,
        topic = CASE WHEN $4 THEN $5 ELSE topic END,
        parent_id = CASE WHEN $6 THEN $7 ELSE parent_id END,
        revision = revision + 1,
        updated_at = $8
    WHERE id = $1
      AND deleted_at = 0
    RETURNING ` + channelColumns

const updateGuildChannelPositionQuery = `
    UPDATE guild_channels
    SET position = $2,
        revision = revision + 1,
        updated_at = $3
    WHERE id = $1
      AND deleted_at = 0
    RETURNING ` + channelColumns

const deleteGuildChannelQuery = `
    UPDATE guild_channels
    SET revision = revision + 1,
        updated_at = $2,
        deleted_at = $2
    WHERE id = $1
      AND deleted_at = 0
    RETURNING ` + channelColumns

const deleteGuildChannelsStatement = `
    UPDATE guild_channels
    SET revision = revision + 1,
        updated_at = $2,
        deleted_at = $2
    WHERE guild_id = $1
      AND deleted_at = 0
`

const clearGuildChannelParentStatement = `
    UPDATE guild_channels
    SET parent_id = 0,
        revision = revision + 1,
        updated_at = $3
    WHERE guild_id = $1
      AND parent_id = $2
      AND deleted_at = 0
`

const upsertGuildChannelPermissionOverwriteQuery = `
    INSERT INTO guild_channel_permission_overwrites (
        channel_id, guild_id, target_type, target_id, allow_bits, deny_bits,
        revision, created_at, updated_at
    ) VALUES ($1, $2, $3, $4, $5, $6, 1, $7, 0)
    ON CONFLICT (channel_id, target_type, target_id) DO UPDATE
    SET allow_bits = EXCLUDED.allow_bits,
        deny_bits = EXCLUDED.deny_bits,
        revision = guild_channel_permission_overwrites.revision + 1,
        updated_at = EXCLUDED.created_at
    RETURNING ` + channelOverwriteColumns

const deleteGuildChannelPermissionOverwriteStatement = `
    DELETE FROM guild_channel_permission_overwrites
    WHERE channel_id = $1
      AND target_type = $2
      AND target_id = $3
`

const deleteGuildChannelPermissionOverwritesStatement = `
    DELETE FROM guild_channel_permission_overwrites
    WHERE channel_id = $1
`

const deleteAllGuildChannelPermissionOverwritesStatement = `
    DELETE FROM guild_channel_permission_overwrites
    WHERE guild_id = $1
`

const deleteGuildChannelPermissionOverwritesForTargetStatement = `
    DELETE FROM guild_channel_permission_overwrites
    WHERE guild_id = $1
      AND target_type = $2
      AND target_id = $3
`

const listGuildChannelPermissionOverwritesQuery = `
    SELECT ` + channelOverwriteColumns + `
    FROM guild_channel_permission_overwrites
    WHERE channel_id = $1
    ORDER BY target_type ASC, target_id ASC
`
