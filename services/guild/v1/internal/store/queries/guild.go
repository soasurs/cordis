package queries

// CreateGuildQuery inserts a guild and returns the stored row.
const CreateGuildQuery = `
    INSERT INTO guilds (
        id, owner_id, name, description, icon_asset_id, revision, created_at, updated_at, deleted_at
    ) VALUES (
        $1, $2, $3, '', 0, 1, $4, 0, 0
    )
    RETURNING ` + GuildColumns

// CreateDefaultRoleStatement inserts the implicit @everyone role for a new
// guild.
const CreateDefaultRoleStatement = `
    INSERT INTO roles (
        id, guild_id, name, permissions, position, is_default,
        revision, created_at, updated_at, deleted_at
    ) VALUES (
        $1, $1, '@everyone', 1120, 0, TRUE, 1, $2, 0, 0
    )
`

// GetGuildForMemberQuery returns a guild only when the user is an active
// member.
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

// ListUserGuildsQuery pages a user's guilds newest-first.
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

// UpdateGuildQuery applies present guild metadata updates and returns the
// stored row.
const UpdateGuildQuery = `
    UPDATE guilds
    SET name = CASE WHEN $2 THEN $3 ELSE name END,
        description = CASE WHEN $4 THEN $5 ELSE description END,
        revision = revision + 1,
        updated_at = $6
    WHERE id = $1
      AND deleted_at = 0
    RETURNING ` + GuildColumns

// UpdateGuildIconQuery replaces a guild's icon asset and returns the stored
// row.
const UpdateGuildIconQuery = `
    UPDATE guilds
    SET icon_asset_id = $2,
        revision = revision + 1,
        updated_at = $3
    WHERE id = $1
      AND deleted_at = 0
    RETURNING ` + GuildColumns

// DeleteGuildQuery soft-deletes a guild and returns the stored row.
const DeleteGuildQuery = `
    UPDATE guilds
    SET revision = revision + 1,
        updated_at = $2,
        deleted_at = $2
    WHERE id = $1
      AND deleted_at = 0
    RETURNING ` + GuildColumns

// DeleteGuildMembersStatement soft-deletes every active member of a guild.
const DeleteGuildMembersStatement = `
    UPDATE guild_members
    SET revision = revision + 1,
        updated_at = $2,
        deleted_at = $2
    WHERE guild_id = $1
      AND deleted_at = 0
`

// DeleteGuildRolesStatement soft-deletes every role in a guild.
const DeleteGuildRolesStatement = `
    UPDATE roles
    SET revision = revision + 1,
        updated_at = $2,
        deleted_at = $2
    WHERE guild_id = $1
      AND deleted_at = 0
`

// GetGuildQuery returns one live guild.
const GetGuildQuery = `
    SELECT ` + GuildColumns + `
    FROM guilds
    WHERE id = $1
      AND deleted_at = 0
    LIMIT 1
`

// GetGuildChannelLayoutRevisionQuery returns a guild's current channel layout
// revision.
const GetGuildChannelLayoutRevisionQuery = `
    SELECT channel_layout_revision
    FROM guilds
    WHERE id = $1
      AND deleted_at = 0
    LIMIT 1
`

// AdvanceGuildChannelLayoutRevisionQuery bumps the channel layout revision
// only from the expected value and returns the new revision.
const AdvanceGuildChannelLayoutRevisionQuery = `
    UPDATE guilds
    SET channel_layout_revision = channel_layout_revision + 1
    WHERE id = $1
      AND deleted_at = 0
      AND channel_layout_revision = $2
    RETURNING channel_layout_revision
`

// CountGuildMembersQuery counts a guild's active members.
const CountGuildMembersQuery = `
    SELECT COUNT(*)
    FROM guild_members
    WHERE guild_id = $1
      AND deleted_at = 0
`

// TransferGuildOwnershipQuery transfers ownership from the current owner and
// returns the stored row.
const TransferGuildOwnershipQuery = `
    UPDATE guilds
    SET owner_id = $3,
        revision = revision + 1,
        updated_at = $4
    WHERE id = $1
      AND owner_id = $2
      AND deleted_at = 0
    RETURNING ` + GuildColumns
