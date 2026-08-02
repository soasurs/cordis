package queries

// CreateGuildRoleQuery inserts a non-default guild role and returns the stored
// row.
const CreateGuildRoleQuery = `
    INSERT INTO roles (
        id, guild_id, name, permissions, position, is_default,
        revision, created_at, updated_at, deleted_at
    ) VALUES (
        $1, $2, $3, $4, $5, FALSE, 1, $6, 0, 0
    )
    RETURNING ` + RoleColumns

// GetGuildRoleQuery returns one live guild role.
const GetGuildRoleQuery = `
    SELECT ` + RoleColumns + `
    FROM roles
    WHERE guild_id = $1
      AND id = $2
      AND deleted_at = 0
    LIMIT 1
`

// ListGuildRolesQuery lists a guild's live roles in position order.
const ListGuildRolesQuery = `
    SELECT ` + RoleColumns + `
    FROM roles
    WHERE guild_id = $1
      AND deleted_at = 0
    ORDER BY position DESC, id ASC
`

// ListGuildRolesByGuildsQuery lists live roles for many guilds in position
// order.
const ListGuildRolesByGuildsQuery = `
    SELECT ` + RoleColumns + `
    FROM roles
    WHERE guild_id = ANY($1)
      AND deleted_at = 0
    ORDER BY guild_id ASC, position DESC, id ASC
`

// UpdateGuildRoleQuery applies present role metadata updates and returns the
// stored row.
const UpdateGuildRoleQuery = `
    UPDATE roles
    SET name = CASE WHEN $3 THEN $4 ELSE name END,
        permissions = CASE WHEN $5 THEN $6 ELSE permissions END,
        revision = revision + 1,
        updated_at = $7
    WHERE guild_id = $1
      AND id = $2
      AND deleted_at = 0
    RETURNING ` + RoleColumns

// UpdateGuildRolePositionQuery moves one non-default role to a new position
// and returns the stored row.
const UpdateGuildRolePositionQuery = `
    UPDATE roles
    SET position = $3,
        revision = revision + 1,
        updated_at = $4
    WHERE guild_id = $1
      AND id = $2
      AND is_default = FALSE
      AND deleted_at = 0
    RETURNING ` + RoleColumns

// DeleteGuildRoleQuery soft-deletes one non-default role and returns the
// stored row.
const DeleteGuildRoleQuery = `
    UPDATE roles
    SET revision = revision + 1,
        updated_at = $3,
        deleted_at = $3
    WHERE guild_id = $1
      AND id = $2
      AND is_default = FALSE
      AND deleted_at = 0
    RETURNING ` + RoleColumns

// AddGuildMemberRoleStatement assigns one role to a member, ignoring duplicate
// assignments.
const AddGuildMemberRoleStatement = `
    INSERT INTO guild_member_roles (guild_id, user_id, role_id, created_at)
    VALUES ($1, $2, $3, $4)
    ON CONFLICT (guild_id, user_id, role_id) DO NOTHING
`

// UpdateGuildRolePositionsQuery atomically applies requested role positions
// and returns the updated rows.
const UpdateGuildRolePositionsQuery = `
    WITH requested(id, position) AS (
        SELECT * FROM unnest($2::bigint[], $3::integer[])
    )
    UPDATE roles AS r
    SET position = requested.position,
        revision = r.revision + 1,
        updated_at = $4
    FROM requested
    WHERE r.guild_id = $1
      AND r.id = requested.id
      AND r.is_default = FALSE
      AND r.deleted_at = 0
    RETURNING r.id, r.guild_id, r.name, r.permissions, r.position, r.is_default,
              r.revision, r.created_at, r.updated_at, r.deleted_at
`

// RemoveGuildMemberRoleStatement removes one role assignment.
const RemoveGuildMemberRoleStatement = `
    DELETE FROM guild_member_roles
    WHERE guild_id = $1
      AND user_id = $2
      AND role_id = $3
`

// DeleteGuildMemberRoleAssignmentsStatement removes every role assignment of
// one member in a guild.
const DeleteGuildMemberRoleAssignmentsStatement = `
    DELETE FROM guild_member_roles
    WHERE guild_id = $1
      AND user_id = $2
`

// DeleteGuildRoleAssignmentsStatement removes every assignment of one role in
// a guild.
const DeleteGuildRoleAssignmentsStatement = `
    DELETE FROM guild_member_roles
    WHERE guild_id = $1
      AND role_id = $2
`

// DeleteAllGuildRoleAssignmentsStatement removes every role assignment in a
// guild.
const DeleteAllGuildRoleAssignmentsStatement = `
    DELETE FROM guild_member_roles
    WHERE guild_id = $1
`

// ListGuildMemberRolesQuery lists a member's roles in a guild, including the
// implicit default role.
const ListGuildMemberRolesQuery = `
    SELECT ` + RoleColumns + `
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

// ListGuildMemberRolesByGuildsQuery lists a user's roles across guilds,
// including implicit default roles.
const ListGuildMemberRolesByGuildsQuery = `
    SELECT ` + RoleColumns + `
    FROM roles
    WHERE guild_id = ANY($1)
      AND deleted_at = 0
      AND (
          is_default = TRUE
          OR id IN (
              SELECT role_id
              FROM guild_member_roles
              WHERE guild_id = ANY($1)
                AND user_id = $2
          )
      )
    ORDER BY guild_id ASC, position DESC, id ASC
`
