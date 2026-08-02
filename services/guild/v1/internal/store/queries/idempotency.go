package queries

const ClaimGuildIdempotencyQuery = `
	INSERT INTO
		guild_idempotency_keys (
			actor_user_id, operation, idempotency_key, request_hash,
			resource_id, created_at, expires_at
		)
	VALUES
		(:actor_user_id, :operation, :idempotency_key, :request_hash,
		 :resource_id, :created_at, :expires_at)
	ON CONFLICT (actor_user_id, operation, idempotency_key) DO NOTHING
	RETURNING
		resource_id, request_hash
	`

const GetGuildIdempotencyQuery = `
	SELECT
		resource_id, request_hash, expires_at
	FROM
		guild_idempotency_keys
	WHERE
		actor_user_id = $1
	AND
		operation = $2
	AND
		idempotency_key = $3
	`

const DeleteExpiredGuildIdempotencyStatement = `
	DELETE FROM
		guild_idempotency_keys
	WHERE
		actor_user_id = $1
	AND
		operation = $2
	AND
		idempotency_key = $3
	AND
		expires_at <= $4
	`
