package queries

// UpsertGuildBanQuery inserts or replaces a guild ban and returns the stored
// row.
const UpsertGuildBanQuery = `
    INSERT INTO guild_bans (guild_id, user_id, actor_user_id, reason, created_at)
    VALUES ($1, $2, $3, $4, $5)
    ON CONFLICT (guild_id, user_id) DO UPDATE
    SET actor_user_id = EXCLUDED.actor_user_id,
        reason = EXCLUDED.reason,
        created_at = EXCLUDED.created_at
    RETURNING ` + GuildBanColumns

// DeleteGuildBanStatement removes one user's ban from a guild.
const DeleteGuildBanStatement = `
    DELETE FROM guild_bans
    WHERE guild_id = $1
      AND user_id = $2
`

// GetGuildBanQuery returns one guild ban.
const GetGuildBanQuery = `
    SELECT ` + GuildBanColumns + `
    FROM guild_bans
    WHERE guild_id = $1
      AND user_id = $2
    LIMIT 1
`

// ListGuildBansQuery pages a guild's bans newest-first.
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

// DeleteGuildBansStatement removes every ban in a guild.
const DeleteGuildBansStatement = `
    DELETE FROM guild_bans
    WHERE guild_id = $1
`
