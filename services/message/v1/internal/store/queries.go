package store

const messageColumns = `
	id, channel_id, author_id, content, type, flags, referenced_message_id,
	referenced_channel_id, attachments, edited_at, created_at, updated_at, deleted_at
`

const (
	CreateMessageQuery = `
	INSERT INTO
		messages (
			id, channel_id, author_id, content, type, flags, referenced_message_id,
			referenced_channel_id, attachments, edited_at, created_at, updated_at, deleted_at
		)
	VALUES
		(
			:id, :channel_id, :author_id, :content, :type, :flags,
			:referenced_message_id, :referenced_channel_id, CAST(:attachments AS JSONB),
			:edited_at, :created_at, :updated_at, :deleted_at
		)
	RETURNING
		id, channel_id, author_id, content, type, flags, referenced_message_id,
		referenced_channel_id, attachments, edited_at, created_at, updated_at, deleted_at
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
		updated_at = $1
	WHERE
		id = $2
	AND
		author_id = $3
	AND
		deleted_at = $4
	`

	// DeleteMessageModStatement skips the author_id check for moderators.
	DeleteMessageModStatement = `
	UPDATE
		messages
	SET
		deleted_at = $1,
		updated_at = $1
	WHERE
		id = $2
	AND
		deleted_at = $3
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

	CreateMessageMentionStatement = `
	INSERT INTO
		message_mentions (message_id, user_id)
	VALUES
		($1, $2)
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

	AddReactionStatement = `
	INSERT INTO
		reactions (message_id, user_id, emoji_id, emoji_name, created_at)
	VALUES
		($1, $2, $3, $4, $5)
	ON CONFLICT DO NOTHING
	`

	RemoveReactionStatement = `
	DELETE FROM
		reactions
	WHERE
		message_id = $1
	AND
		user_id = $2
	AND
		emoji_id = $3
	AND
		emoji_name = $4
	`

	ListReactionSummariesQuery = `
	SELECT
		message_id,
		emoji_id,
		emoji_name,
		COUNT(*)::BIGINT AS count,
		BOOL_OR(user_id = $2) AS me
	FROM
		reactions
	WHERE
		message_id = ANY($1)
	GROUP BY
		message_id, emoji_id, emoji_name
	ORDER BY
		message_id DESC, count DESC, emoji_id ASC, emoji_name ASC
	`

	ListReactionUsersQuery = `
	SELECT
		user_id
	FROM
		reactions
	WHERE
		message_id = $1
	AND
		emoji_id = $2
	AND
		emoji_name = $3
	AND
		user_id > $4
	ORDER BY
		user_id ASC
	LIMIT
		$5
	`
)
