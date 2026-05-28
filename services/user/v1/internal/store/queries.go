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
		user_id, email, hashed_password, created_at, updated_at, deleted_at
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
		user_id, email, hashed_password, created_at, updated_at, deleted_at
	FROM 
		users
	WHERE
		email = $1
	AND
		deleted_at = $2
	LIMIT
		1
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
)
