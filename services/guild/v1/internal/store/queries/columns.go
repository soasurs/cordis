package queries

// GuildColumns lists the guilds table columns selected by guild queries.
const GuildColumns = `
    id, owner_id, name, description, icon_asset_id, revision, access_revision,
    channel_layout_revision, created_at, updated_at, deleted_at
`

// GuildMemberColumns lists the guild_members table columns selected by member
// queries.
const GuildMemberColumns = `
    guild_id, user_id, nickname, revision, joined_at, updated_at, deleted_at
`

// GuildBanColumns lists the guild_bans table columns selected by ban queries.
const GuildBanColumns = `
    guild_id, user_id, actor_user_id, reason, created_at
`

// RoleColumns lists the roles table columns selected by role queries.
const RoleColumns = `
    id, guild_id, name, permissions, position, is_default,
    revision, created_at, updated_at, deleted_at
`

// ChannelColumns lists the guild_channels table columns selected by channel
// queries.
const ChannelColumns = `
    id, guild_id, name, type, position, topic, revision, created_at, updated_at, deleted_at, parent_id
`

// ChannelOverwriteColumns lists the guild_channel_permission_overwrites table
// columns selected by overwrite queries.
const ChannelOverwriteColumns = `
    channel_id, guild_id, applies_to, applies_to_id, allow_bits, deny_bits,
    revision, created_at, updated_at
`
