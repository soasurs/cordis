package queries

// UpsertGuildChannelPermissionOverwriteQuery inserts or replaces one channel
// overwrite and returns the stored row.
const UpsertGuildChannelPermissionOverwriteQuery = `
    INSERT INTO guild_channel_permission_overwrites (
        channel_id, guild_id, applies_to, applies_to_id, allow_bits, deny_bits,
        revision, created_at, updated_at
    ) VALUES ($1, $2, $3, $4, $5, $6, 1, $7, 0)
    ON CONFLICT (channel_id, applies_to, applies_to_id) DO UPDATE
    SET allow_bits = EXCLUDED.allow_bits,
        deny_bits = EXCLUDED.deny_bits,
        revision = guild_channel_permission_overwrites.revision + 1,
        updated_at = EXCLUDED.created_at
    RETURNING ` + ChannelOverwriteColumns

// DeleteGuildChannelPermissionOverwriteStatement removes one channel
// overwrite.
const DeleteGuildChannelPermissionOverwriteStatement = `
    DELETE FROM guild_channel_permission_overwrites
    WHERE channel_id = $1
      AND applies_to = $2
      AND applies_to_id = $3
`

// DeleteGuildChannelPermissionOverwritesStatement removes every overwrite on
// a channel.
const DeleteGuildChannelPermissionOverwritesStatement = `
    DELETE FROM guild_channel_permission_overwrites
    WHERE channel_id = $1
`

// DeleteAllGuildChannelPermissionOverwritesStatement removes every overwrite
// in a guild.
const DeleteAllGuildChannelPermissionOverwritesStatement = `
    DELETE FROM guild_channel_permission_overwrites
    WHERE guild_id = $1
`

// DeleteGuildChannelPermissionOverwritesForAppliesToStatement removes every
// guild overwrite that applies to one role or member.
const DeleteGuildChannelPermissionOverwritesForAppliesToStatement = `
    DELETE FROM guild_channel_permission_overwrites
    WHERE guild_id = $1
      AND applies_to = $2
      AND applies_to_id = $3
`

// ListGuildChannelPermissionOverwritesQuery lists the overwrites on one
// channel.
const ListGuildChannelPermissionOverwritesQuery = `
    SELECT ` + ChannelOverwriteColumns + `
    FROM guild_channel_permission_overwrites
    WHERE channel_id = $1
    ORDER BY applies_to ASC, applies_to_id ASC
`

// ListGuildChannelPermissionOverwritesByChannelsQuery lists the overwrites on
// many channels.
const ListGuildChannelPermissionOverwritesByChannelsQuery = `
    SELECT ` + ChannelOverwriteColumns + `
    FROM guild_channel_permission_overwrites
    WHERE channel_id = ANY($1)
    ORDER BY guild_id ASC, channel_id ASC, applies_to ASC, applies_to_id ASC
`

// ListGuildChannelPermissionOverwritesByGuildQuery lists every overwrite in a
// guild.
const ListGuildChannelPermissionOverwritesByGuildQuery = `
    SELECT ` + ChannelOverwriteColumns + `
    FROM guild_channel_permission_overwrites
    WHERE guild_id = $1
    ORDER BY channel_id ASC, applies_to ASC, applies_to_id ASC
`

// ListGuildChannelPermissionOverwritesByGuildsQuery lists overwrites relevant
// to one user across many guilds.
const ListGuildChannelPermissionOverwritesByGuildsQuery = `
    SELECT ` + ChannelOverwriteColumns + `
    FROM guild_channel_permission_overwrites AS o
    WHERE o.guild_id = ANY($1)
      AND (
          (o.applies_to = 2 AND o.applies_to_id = $2)
          OR (o.applies_to = 1 AND (
              o.applies_to_id = o.guild_id
              OR EXISTS (
                  SELECT 1
                  FROM guild_member_roles AS mr
                  WHERE mr.guild_id = o.guild_id
                    AND mr.user_id = $2
                    AND mr.role_id = o.applies_to_id
              )
          ))
      )
    ORDER BY guild_id ASC, channel_id ASC, applies_to ASC, applies_to_id ASC
`
