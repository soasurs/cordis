package store

const messageColumns = `
	id, channel_id, author_id, content, type, flags, referenced_message_id,
	referenced_channel_id, attachments, edited_at, created_at, updated_at, revision, deleted_at
`

const (
	CreateMessageQuery = `
	INSERT INTO
		messages (
			id, channel_id, author_id, content, type, flags, referenced_message_id,
			referenced_channel_id, attachments, edited_at, created_at, updated_at, revision, deleted_at
		)
	VALUES
		(
			:id, :channel_id, :author_id, :content, :type, :flags,
			:referenced_message_id, :referenced_channel_id, CAST(:attachments AS JSONB),
			:edited_at, :created_at, :updated_at, :revision, :deleted_at
		)
	RETURNING
		id, channel_id, author_id, content, type, flags, referenced_message_id,
		referenced_channel_id, attachments, edited_at, created_at, updated_at, revision, deleted_at
	`

	GetMessageQuery = `
	SELECT
		` + messageColumns + `
	FROM
		messages
	WHERE
		id = $1
	AND
		deleted_at = $2
	LIMIT
		1
	`

	ListNewestMessagesQuery = `
	SELECT
		` + messageColumns + `
	FROM
		messages
	WHERE
		channel_id = $1
	AND
		deleted_at = $2
	ORDER BY
		id DESC
	LIMIT
		$3
	`

	ListMessagesBeforeQuery = `
	SELECT
		` + messageColumns + `
	FROM
		messages
	WHERE
		channel_id = $1
	AND
		deleted_at = $2
	AND
		id < $3
	ORDER BY
		id DESC
	LIMIT
		$4
	`

	ListMessagesAfterQuery = `
	SELECT
		` + messageColumns + `
	FROM
		messages
	WHERE
		channel_id = $1
	AND
		deleted_at = $2
	AND
		id > $3
	ORDER BY
		id ASC
	LIMIT
		$4
	`

	ListMessagesAroundOlderQuery = `
	SELECT
		` + messageColumns + `
	FROM
		messages
	WHERE
		channel_id = $1
	AND
		deleted_at = $2
	AND
		id <= $3
	ORDER BY
		id DESC
	LIMIT
		$4
	`

	ListMessagesAroundNewerQuery = `
	SELECT
		` + messageColumns + `
	FROM
		messages
	WHERE
		channel_id = $1
	AND
		deleted_at = $2
	AND
		id > $3
	ORDER BY
		id ASC
	LIMIT
		$4
	`

	DeleteMessageStatement = `
	UPDATE
		messages
	SET
		deleted_at = $1,
		updated_at = $1,
		revision = revision + 1
	WHERE
		id = $2
	AND
		author_id = $3
	AND
		deleted_at = $4
	RETURNING
		` + messageColumns + `
	`

	// DeleteMessageModStatement skips the author_id check for moderators.
	DeleteMessageModStatement = `
	UPDATE
		messages
	SET
		deleted_at = $1,
		updated_at = $1,
		revision = revision + 1
	WHERE
		id = $2
	AND
		deleted_at = $3
	RETURNING
		` + messageColumns + `
	`

	CheckMessageExistsQuery = `
	SELECT
		EXISTS (
			SELECT
				1
			FROM
				messages
			WHERE
				id = $1
			AND
				deleted_at = $2
		)
	`

	DeleteMessageMentionsStatement = `
	DELETE FROM
		message_mentions
	WHERE
		message_id = $1
	`

	InsertMessageMentionsStatement = `
	INSERT INTO
		message_mentions (message_id, user_id)
	SELECT
		$1, mention.user_id
	FROM
		unnest($2::BIGINT[]) AS mention(user_id)
	ON CONFLICT DO NOTHING
	`

	ListMessageMentionsQuery = `
	SELECT
		user_id
	FROM
		message_mentions
	WHERE
		message_id = $1
	ORDER BY
		user_id ASC
	`
)

const createDmChannelStatement = `
	INSERT INTO dm_channels (id, user_lo, user_hi, created_at)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (user_lo, user_hi) DO NOTHING
`

const getDmChannelQuery = `
	SELECT id, user_lo, user_hi, created_at
	FROM dm_channels
	WHERE id = $1
	LIMIT 1
`

const getDmChannelByPairQuery = `
	SELECT id, user_lo, user_hi, created_at
	FROM dm_channels
	WHERE user_lo = $1 AND user_hi = $2
	LIMIT 1
`

const listDmChannelsQuery = `
	SELECT id, user_lo, user_hi, created_at
	FROM dm_channels
	WHERE (user_lo = $1 OR user_hi = $1)
	  AND ($2 = 0 OR id < $2)
	ORDER BY id DESC
	LIMIT $3
`

const listAllDmChannelsQuery = `
	SELECT id, user_lo, user_hi, created_at
	FROM dm_channels
	WHERE user_lo = $1 OR user_hi = $1
	ORDER BY id DESC
`

const ackMessageQuery = `
	WITH target AS (
		SELECT channel_id, id
		FROM messages
		WHERE id = $3 AND channel_id = $2
	), advanced AS (
		INSERT INTO channel_read_states (user_id, channel_id, last_read_message_id, updated_at)
		SELECT $1, target.channel_id, target.id, $4
		FROM target
		ON CONFLICT (user_id, channel_id) DO UPDATE SET
			last_read_message_id = EXCLUDED.last_read_message_id,
			updated_at = EXCLUDED.updated_at
		WHERE channel_read_states.last_read_message_id < EXCLUDED.last_read_message_id
		RETURNING 1
	)
	SELECT
		EXISTS (SELECT 1 FROM target) AS target_exists,
		EXISTS (SELECT 1 FROM advanced) AS advanced
`

const listReadyChannelReadStatesQuery = `
	WITH requested AS (
		SELECT channel_id, ordinal
		FROM unnest($2::bigint[]) WITH ORDINALITY AS requested(channel_id, ordinal)
	),
	watermarks AS (
		SELECT
			requested.channel_id,
			requested.ordinal,
			COALESCE(states.last_read_message_id, 0) AS last_read_message_id,
			COALESCE(states.updated_at, 0) AS updated_at
		FROM requested
		LEFT JOIN channel_read_states AS states
			ON states.user_id = $1
			AND states.channel_id = requested.channel_id
	),
	latest_messages AS (
		SELECT watermarks.channel_id, latest.id AS last_message_id
		FROM watermarks
		LEFT JOIN LATERAL (
			SELECT messages.id
			FROM messages
			WHERE messages.channel_id = watermarks.channel_id
				AND messages.deleted_at = 0
			ORDER BY messages.id DESC
			LIMIT 1
		) AS latest ON TRUE
	),
	mention_counts AS (
		SELECT messages.channel_id, count(*)::int AS mention_count
		FROM message_mentions AS mentions
		JOIN messages ON messages.id = mentions.message_id
		JOIN watermarks
			ON watermarks.channel_id = messages.channel_id
			AND messages.id > watermarks.last_read_message_id
		WHERE mentions.user_id = $1
			AND messages.deleted_at = 0
		GROUP BY messages.channel_id
	)
	SELECT
		$1 AS user_id,
		watermarks.channel_id,
		COALESCE(latest_messages.last_message_id, 0) AS last_message_id,
		watermarks.last_read_message_id,
		COALESCE(mention_counts.mention_count, 0) AS mention_count,
		watermarks.updated_at
	FROM watermarks
	LEFT JOIN latest_messages ON latest_messages.channel_id = watermarks.channel_id
	LEFT JOIN mention_counts ON mention_counts.channel_id = watermarks.channel_id
	ORDER BY watermarks.ordinal
`

const getLastMessageIDQuery = `
	SELECT id
	FROM messages
	WHERE channel_id = $1
		AND deleted_at = 0
	ORDER BY id DESC
	LIMIT 1
`
