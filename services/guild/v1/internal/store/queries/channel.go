package queries

const CreateGuildChannelQuery = `
    INSERT INTO guild_channels (
        id, guild_id, name, type, position, topic, revision, created_at, updated_at, deleted_at, parent_id
    ) VALUES ($1, $2, $3, $4, $5, $6, 1, $8, 0, 0, $7)
    RETURNING ` + ChannelColumns

const GetGuildChannelQuery = `
    SELECT ` + ChannelColumns + `
    FROM guild_channels
    WHERE id = $1
      AND deleted_at = 0
    LIMIT 1
`

const ListGuildChannelsQuery = `
    SELECT ` + ChannelColumns + `
    FROM guild_channels
    WHERE guild_id = $1
      AND deleted_at = 0
    ORDER BY position ASC, id ASC
`

const ListGuildChannelsWithRevisionQuery = `
    SELECT
        g.id AS snapshot_guild_id,
        g.channel_layout_revision,
        (c.id IS NOT NULL) AS has_channel,
        COALESCE(c.id, 0) AS id,
        COALESCE(c.guild_id, 0) AS guild_id,
        COALESCE(c.name, '') AS name,
        COALESCE(c.type, 0) AS type,
        COALESCE(c.position, 0) AS position,
        COALESCE(c.topic, '') AS topic,
        COALESCE(c.revision, 0) AS revision,
        COALESCE(c.created_at, 0) AS created_at,
        COALESCE(c.updated_at, 0) AS updated_at,
        COALESCE(c.deleted_at, 0) AS deleted_at,
        COALESCE(c.parent_id, 0) AS parent_id
    FROM guilds AS g
    LEFT JOIN guild_channels AS c
      ON c.guild_id = g.id
     AND c.deleted_at = 0
    WHERE g.id = $1
      AND g.deleted_at = 0
    ORDER BY c.position ASC, c.id ASC
`

const ListGuildChannelsWithRevisionsByGuildsQuery = `
    SELECT
        g.id AS snapshot_guild_id,
        g.channel_layout_revision,
        (c.id IS NOT NULL) AS has_channel,
        COALESCE(c.id, 0) AS id,
        COALESCE(c.guild_id, 0) AS guild_id,
        COALESCE(c.name, '') AS name,
        COALESCE(c.type, 0) AS type,
        COALESCE(c.position, 0) AS position,
        COALESCE(c.topic, '') AS topic,
        COALESCE(c.revision, 0) AS revision,
        COALESCE(c.created_at, 0) AS created_at,
        COALESCE(c.updated_at, 0) AS updated_at,
        COALESCE(c.deleted_at, 0) AS deleted_at,
        COALESCE(c.parent_id, 0) AS parent_id
    FROM guilds AS g
    LEFT JOIN guild_channels AS c
      ON c.guild_id = g.id
     AND c.deleted_at = 0
    WHERE g.id = ANY($1)
      AND g.deleted_at = 0
    ORDER BY g.id ASC, c.position ASC, c.id ASC
`

const ListGuildChannelsByGuildsQuery = `
    SELECT ` + ChannelColumns + `
    FROM guild_channels
    WHERE guild_id = ANY($1)
      AND deleted_at = 0
    ORDER BY guild_id ASC, position ASC, id ASC
`

const UpdateGuildChannelQuery = `
    UPDATE guild_channels
    SET name = CASE WHEN $2 THEN $3 ELSE name END,
        topic = CASE WHEN $4 THEN $5 ELSE topic END,
        parent_id = CASE WHEN $6 THEN $7 ELSE parent_id END,
        revision = revision + 1,
        updated_at = $8
    WHERE id = $1
      AND deleted_at = 0
    RETURNING ` + ChannelColumns

const UpdateGuildChannelPositionQuery = `
    UPDATE guild_channels
    SET position = $3,
        revision = revision + 1,
        updated_at = $4
    WHERE guild_id = $1
      AND id = $2
      AND deleted_at = 0
    RETURNING ` + ChannelColumns

const DeleteGuildChannelQuery = `
    UPDATE guild_channels
    SET revision = revision + 1,
        updated_at = $2,
        deleted_at = $2
    WHERE id = $1
      AND deleted_at = 0
    RETURNING ` + ChannelColumns

const DeleteGuildChannelsStatement = `
    UPDATE guild_channels
    SET revision = revision + 1,
        updated_at = $2,
        deleted_at = $2
    WHERE guild_id = $1
      AND deleted_at = 0
`

const UpdateGuildChannelPositionsQuery = `
    WITH requested(id, position, parent_id) AS (
        SELECT * FROM unnest($2::bigint[], $3::integer[], $4::bigint[])
    )
    UPDATE guild_channels AS c
    SET position = requested.position,
        parent_id = requested.parent_id,
        revision = c.revision + 1,
        updated_at = $5
    FROM requested
    WHERE c.guild_id = $1
      AND c.id = requested.id
      AND c.deleted_at = 0
    RETURNING c.id, c.guild_id, c.name, c.type, c.position, c.topic,
              c.revision, c.created_at, c.updated_at, c.deleted_at, c.parent_id
`

const ClearGuildChannelParentStatement = `
    UPDATE guild_channels
    SET parent_id = 0,
        revision = revision + 1,
        updated_at = $3
    WHERE guild_id = $1
      AND parent_id = $2
      AND deleted_at = 0
`
