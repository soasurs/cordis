package queries

// GuildInviteColumns lists the guild_invites table columns selected by invite
// queries.
const GuildInviteColumns = `
    id, code, guild_id, creator_user_id, max_uses, uses, expires_at, created_at
`

// CreateGuildInviteQuery inserts a guild invite and returns the stored row.
const CreateGuildInviteQuery = `
    INSERT INTO guild_invites (
        id, code, guild_id, creator_user_id, max_uses, uses, expires_at, created_at
    ) VALUES ($1, $2, $3, $4, $5, 0, $6, $7)
    RETURNING ` + GuildInviteColumns

// GetGuildInviteQuery returns an invite by its code.
const GetGuildInviteQuery = `
    SELECT ` + GuildInviteColumns + `
    FROM guild_invites
    WHERE code = $1
    LIMIT 1
`

// GetGuildInviteByIDQuery returns an invite by its ID.
const GetGuildInviteByIDQuery = `
    SELECT ` + GuildInviteColumns + `
    FROM guild_invites
    WHERE id = $1
    LIMIT 1
`

// ListGuildInvitesQuery pages a guild's invites newest-first.
const ListGuildInvitesQuery = `
    SELECT ` + GuildInviteColumns + `
    FROM guild_invites
    WHERE guild_id = $1
      AND ($2::BIGINT = 0 OR id < $2::BIGINT)
    ORDER BY id DESC
    LIMIT $3
`

// ConsumeGuildInviteQuery increments an invite's use count when it is still
// usable and returns the row.
const ConsumeGuildInviteQuery = `
    UPDATE guild_invites
    SET uses = uses + 1
    WHERE code = $1
      AND (max_uses = 0 OR uses < max_uses)
      AND (expires_at = 0 OR expires_at > $2)
    RETURNING ` + GuildInviteColumns

// DeleteGuildInviteStatement removes one invite by code.
const DeleteGuildInviteStatement = `
    DELETE FROM guild_invites
    WHERE code = $1
`

// DeleteGuildInvitesStatement removes every invite in a guild.
const DeleteGuildInvitesStatement = `
    DELETE FROM guild_invites
    WHERE guild_id = $1
`
