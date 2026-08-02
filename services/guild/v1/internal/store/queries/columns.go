package queries

const GuildColumns = `
    id, owner_id, name, description, icon_asset_id, revision, access_revision,
    channel_layout_revision, created_at, updated_at, deleted_at
`

const GuildMemberColumns = `
    guild_id, user_id, nickname, revision, joined_at, updated_at, deleted_at
`

const GuildBanColumns = `
    guild_id, user_id, actor_user_id, reason, created_at
`

const RoleColumns = `
    id, guild_id, name, permissions, position, is_default,
    revision, created_at, updated_at, deleted_at
`

const ChannelColumns = `
    id, guild_id, name, type, position, topic, revision, created_at, updated_at, deleted_at, parent_id
`

const ChannelOverwriteColumns = `
    channel_id, guild_id, applies_to, applies_to_id, allow_bits, deny_bits,
    revision, created_at, updated_at
`
