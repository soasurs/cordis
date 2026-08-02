package queries

const UpsertGuildBanQuery = `
    INSERT INTO guild_bans (guild_id, user_id, actor_user_id, reason, created_at)
    VALUES ($1, $2, $3, $4, $5)
    ON CONFLICT (guild_id, user_id) DO UPDATE
    SET actor_user_id = EXCLUDED.actor_user_id,
        reason = EXCLUDED.reason,
        created_at = EXCLUDED.created_at
    RETURNING ` + GuildBanColumns

const DeleteGuildBanStatement = `
    DELETE FROM guild_bans
    WHERE guild_id = $1
      AND user_id = $2
`

const GetGuildBanQuery = `
    SELECT ` + GuildBanColumns + `
    FROM guild_bans
    WHERE guild_id = $1
      AND user_id = $2
    LIMIT 1
`

const ListGuildBansQuery = `
    SELECT ` + GuildBanColumns + `
    FROM guild_bans
    WHERE guild_id = $1
      AND (
          $2::BIGINT = 0
          OR created_at < $2::BIGINT
          OR (created_at = $2::BIGINT AND user_id < $3::BIGINT)
      )
    ORDER BY created_at DESC, user_id DESC
    LIMIT $4
`

const DeleteGuildBansStatement = `
    DELETE FROM guild_bans
    WHERE guild_id = $1
`
