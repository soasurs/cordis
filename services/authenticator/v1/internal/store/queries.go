package store

const (
	CreateSessionStatement = `
	INSERT INTO
		sessions (session_id, user_id, refresh_token_hash, user_agent, ip, created_at, updated_at, expires_at, revoked_at)
	VALUES
		(:session_id, :user_id, :refresh_token_hash, :user_agent, :ip, :created_at, :updated_at, :expires_at, :revoked_at);
	`

	GetSessionQuery = `
	SELECT
		session_id, user_id, refresh_token_hash, user_agent, ip, created_at, updated_at, expires_at, revoked_at
	FROM
		sessions
	WHERE
		session_id = $1
	LIMIT
		1
	`

	ListSessionsQuery = `
	SELECT
		session_id, user_id, refresh_token_hash, user_agent, ip, created_at, updated_at, expires_at, revoked_at
	FROM
		sessions
	WHERE
		user_id = $1
	AND
		revoked_at = $2
	AND
		expires_at > $3
	ORDER BY
		created_at DESC
	`

	RotateRefreshTokenStatement = `
	UPDATE
		sessions
	SET
		refresh_token_hash = $1,
		updated_at = $2
	WHERE
		session_id = $3
	AND
		revoked_at = $4
	AND
		refresh_token_hash = $5
	`

	RevokeSessionStatement = `
	UPDATE
		sessions
	SET
		revoked_at = $1,
		updated_at = $1
	WHERE
		session_id = $2
	AND
		revoked_at = $3
	`

	RevokeUserSessionStatement = `
	UPDATE
		sessions
	SET
		revoked_at = $1,
		updated_at = $1
	WHERE
		user_id = $2
	AND
		session_id = $3
	AND
		revoked_at = $4
	`

	RevokeOtherSessionsStatement = `
	UPDATE
		sessions
	SET
		revoked_at = $1,
		updated_at = $1
	WHERE
		user_id = $2
	AND
		session_id != $3
	AND
		revoked_at = $4
	`
)
