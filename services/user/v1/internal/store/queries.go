package store

const (
	CreateUserStatement = `
	INSERT INTO 
		users (user_id, email, hashed_password, created_at, updated_at, deleted_at)
	VALUES 
		(:user_id, :email, :hashed_password, :created_at, :updated_at, :deleted_at);
	`

	GetUserQuery = `
	SELECT 
		user_id, email, hashed_password, created_at, updated_at, deleted_at, email_verified_at
	FROM 
		users
	WHERE
		user_id = $1
	AND
		deleted_at = $2
	LIMIT
		1
	`

	GetUserWithEmailQuery = `
	SELECT
		user_id, email, hashed_password, created_at, updated_at, deleted_at, email_verified_at
	FROM 
		users
	WHERE
		email = $1
	AND
		deleted_at = $2
	LIMIT
		1
	`

	CheckEmailAvailabilityQuery = `
	SELECT
		NOT EXISTS (
			SELECT
				1
			FROM
				users
			WHERE
				email = $1
			AND
				deleted_at = $2
		)
	`

	UpdateUserPasswordStatement = `
	UPDATE
		users
	SET
		hashed_password = $1,
		updated_at = $2
	WHERE
		user_id = $3
	AND
		deleted_at = $4
	`

	UpdateUserEmailQuery = `
	UPDATE
		users
	SET
		updated_at = CASE WHEN email = $1 THEN updated_at ELSE $2 END,
		email_verified_at = CASE WHEN email = $1 THEN email_verified_at ELSE 0 END,
		email = $1
	WHERE
		user_id = $3
	AND
		deleted_at = $4
	RETURNING
		user_id, email, hashed_password, created_at, updated_at, deleted_at, email_verified_at
	`

	MarkUserEmailVerifiedStatement = `
	UPDATE
		users
	SET
		email_verified_at = $1,
		updated_at = $2
	WHERE
		user_id = $3
	AND
		email = $4
	AND
		deleted_at = $5
	`
)

const (
	CreateUserProfileStatement = `
	INSERT INTO
		user_profiles (user_id, name, avatar_uri, created_at, updated_at, deleted_at)
	VALUES
		(:user_id, :name, :avatar_uri, :created_at, :updated_at, :deleted_at);
	`

	GetUserProfileQuery = `
	SELECT
		user_id, name, avatar_uri, created_at, updated_at, deleted_at
	FROM
		user_profiles
	WHERE
		user_id = $1
	AND
		deleted_at = $2
	LIMIT
		1
	`

	UpdateUserProfileQuery = `
	UPDATE
		user_profiles
	SET
		name = $1,
		avatar_uri = $2,
		updated_at = $3
	WHERE
		user_id = $4
	AND
		deleted_at = $5
	RETURNING
		user_id, name, avatar_uri, created_at, updated_at, deleted_at
	`
)
