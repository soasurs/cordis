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
