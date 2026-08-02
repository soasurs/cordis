package queries

const CreateGuildMemberQuery = `
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
    RETURNING ` + GuildMemberColumns

const GetGuildMemberQuery = `
    SELECT ` + GuildMemberColumns + `
    FROM guild_members
    WHERE guild_id = $1
      AND user_id = $2
      AND deleted_at = 0
    LIMIT 1
`

const ListGuildMembersQuery = `
    SELECT ` + GuildMemberColumns + `
    FROM guild_members
    WHERE guild_id = $1
      AND deleted_at = 0
      AND (
          $2::BIGINT = 0
          OR joined_at < $2::BIGINT
          OR (joined_at = $2::BIGINT AND user_id < $3::BIGINT)
      )
    ORDER BY joined_at DESC, user_id DESC
    LIMIT $4
`

const ListGuildMemberIDsPageQuery = `
    SELECT user_id
    FROM guild_members
    WHERE guild_id = $1
      AND deleted_at = 0
      AND user_id > $2
    ORDER BY user_id
    LIMIT $3
`

const ListGuildRoleTargetIDsPageQuery = `
    SELECT DISTINCT gm.user_id
    FROM guild_member_roles AS gm
    JOIN guild_members AS m
      ON m.guild_id = gm.guild_id
     AND m.user_id = gm.user_id
     AND m.deleted_at = 0
    WHERE gm.guild_id = $1
      AND gm.role_id = ANY($2::BIGINT[])
      AND gm.user_id > $3
    ORDER BY gm.user_id
    LIMIT $4
`

const ListGuildMemberRolesByUsersQuery = `
    SELECT r.*, assignments.user_id
    FROM (
        SELECT guild_id, user_id, role_id
        FROM guild_member_roles
        WHERE guild_id = $1
          AND user_id = ANY($2::BIGINT[])
        UNION ALL
        SELECT guild_id, user_id, guild_id AS role_id
        FROM guild_members
        WHERE guild_id = $1
          AND user_id = ANY($2::BIGINT[])
          AND deleted_at = 0
    ) AS assignments
    JOIN roles AS r
      ON r.guild_id = assignments.guild_id
     AND r.id = assignments.role_id
    WHERE r.deleted_at = 0
`

const ListUsersWithCommonGuildQuery = `
    SELECT DISTINCT target.user_id
    FROM guild_members AS actor
    JOIN guild_members AS target
      ON target.guild_id = actor.guild_id
     AND target.deleted_at = 0
    WHERE actor.user_id = $1
      AND actor.deleted_at = 0
      AND target.user_id = ANY($2)
    ORDER BY target.user_id
`

const ListGuildRoleMembersQuery = `
    SELECT gm.guild_id, gm.user_id, gm.nickname, gm.revision,
           gm.joined_at, gm.updated_at, gm.deleted_at
    FROM guild_members AS gm
    WHERE gm.guild_id = $1
      AND gm.deleted_at = 0
      AND (
          $3::BIGINT = 0
          OR gm.joined_at < $3::BIGINT
          OR (gm.joined_at = $3::BIGINT AND gm.user_id < $4::BIGINT)
      )
      AND EXISTS (
          SELECT 1
          FROM roles AS r
          WHERE r.guild_id = gm.guild_id
            AND r.id = $2
            AND r.deleted_at = 0
            AND (
                r.is_default = TRUE
                OR EXISTS (
                    SELECT 1
                    FROM guild_member_roles AS gmr
                    WHERE gmr.guild_id = gm.guild_id
                      AND gmr.user_id = gm.user_id
                      AND gmr.role_id = r.id
                )
            )
      )
    ORDER BY gm.joined_at DESC, gm.user_id DESC
    LIMIT $5
`

const UpdateGuildMemberNicknameQuery = `
    UPDATE guild_members
    SET nickname = $3,
        revision = revision + 1,
        updated_at = $4
    WHERE guild_id = $1
      AND user_id = $2
      AND deleted_at = 0
    RETURNING ` + GuildMemberColumns

const RemoveGuildMemberQuery = `
    UPDATE guild_members
    SET revision = revision + 1,
        updated_at = $3,
        deleted_at = $3
    WHERE guild_id = $1
      AND user_id = $2
      AND deleted_at = 0
    RETURNING ` + GuildMemberColumns
