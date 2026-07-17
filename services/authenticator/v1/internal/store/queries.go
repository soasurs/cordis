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

	GetTOTPFactorQuery = `
	SELECT user_id, secret_ciphertext, encryption_key_id, last_used_counter, enabled_at, created_at, updated_at
	FROM user_totp_factors
	WHERE user_id = $1
	LIMIT 1`

	CreateTOTPEnrollmentStatement = `
	INSERT INTO totp_enrollments (user_id, token_hash, secret_ciphertext, encryption_key_id, created_at, expires_at)
	VALUES ($1, $2, $3, $4, $5, $6)
	ON CONFLICT (user_id) DO UPDATE SET
		token_hash = EXCLUDED.token_hash,
		secret_ciphertext = EXCLUDED.secret_ciphertext,
		encryption_key_id = EXCLUDED.encryption_key_id,
		created_at = EXCLUDED.created_at,
		expires_at = EXCLUDED.expires_at
	WHERE totp_enrollments.expires_at <= $7`

	GetTOTPEnrollmentQuery = `
	SELECT user_id, token_hash, secret_ciphertext, encryption_key_id, created_at, expires_at
	FROM totp_enrollments
	WHERE user_id = $1 AND token_hash = $2
	LIMIT 1`

	DeleteTOTPEnrollmentStatement = `
	DELETE FROM totp_enrollments WHERE user_id = $1 AND token_hash = $2`

	UpsertTOTPFactorStatement = `
	INSERT INTO user_totp_factors (user_id, secret_ciphertext, encryption_key_id, last_used_counter, enabled_at, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	ON CONFLICT (user_id) DO UPDATE SET
		secret_ciphertext = EXCLUDED.secret_ciphertext,
		encryption_key_id = EXCLUDED.encryption_key_id,
		last_used_counter = EXCLUDED.last_used_counter,
		enabled_at = EXCLUDED.enabled_at,
		updated_at = EXCLUDED.updated_at`

	DeleteTOTPFactorStatement = `
	DELETE FROM user_totp_factors WHERE user_id = $1`

	UpdateTOTPLastUsedCounterStatement = `
	UPDATE user_totp_factors
	SET last_used_counter = $1, updated_at = $2
	WHERE user_id = $3 AND last_used_counter < $4`

	CreateTwoFactorLoginChallengeStatement = `
	INSERT INTO two_factor_login_challenges (token_hash, user_id, user_agent, ip, attempts, created_at, expires_at, consumed_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	GetTwoFactorLoginChallengeQuery = `
	SELECT token_hash, user_id, user_agent, ip, attempts, created_at, expires_at, consumed_at
	FROM two_factor_login_challenges
	WHERE token_hash = $1
	LIMIT 1`

	IncrementTwoFactorLoginChallengeAttemptsStatement = `
	UPDATE two_factor_login_challenges SET attempts = attempts + 1 WHERE token_hash = $1 AND consumed_at = 0`

	ConsumeTwoFactorLoginChallengeStatement = `
	UPDATE two_factor_login_challenges SET consumed_at = $1 WHERE token_hash = $2 AND consumed_at = 0`

	DeleteRecoveryCodesStatement = `
	DELETE FROM two_factor_recovery_codes WHERE user_id = $1`

	CreateRecoveryCodeStatement = `
	INSERT INTO two_factor_recovery_codes (user_id, code_hash, created_at, used_at)
	VALUES ($1, $2, $3, $4)`

	CountUnusedRecoveryCodesQuery = `
	SELECT count(*) FROM two_factor_recovery_codes WHERE user_id = $1 AND used_at = 0`

	ConsumeRecoveryCodeStatement = `
	UPDATE two_factor_recovery_codes
	SET used_at = $1
	WHERE user_id = $2 AND code_hash = $3 AND used_at = $4`

	UpsertPasswordResetTokenStatement = `
	INSERT INTO password_reset_tokens (user_id, token_hash, created_at, expires_at, consumed_at)
	VALUES ($1, $2, $3, $4, 0)
	ON CONFLICT (user_id) DO UPDATE SET
		token_hash = EXCLUDED.token_hash,
		created_at = EXCLUDED.created_at,
		expires_at = EXCLUDED.expires_at,
		consumed_at = 0`

	GetPasswordResetTokenQuery = `
	SELECT user_id, token_hash, created_at, expires_at, consumed_at
	FROM password_reset_tokens
	WHERE token_hash = $1
	LIMIT 1`

	ConsumePasswordResetTokenStatement = `
	UPDATE password_reset_tokens SET consumed_at = $1 WHERE token_hash = $2 AND consumed_at = 0`

	UpsertEmailVerificationTokenStatement = `
	INSERT INTO email_verification_tokens (user_id, token_hash, email, created_at, expires_at, consumed_at)
	VALUES ($1, $2, $3, $4, $5, 0)
	ON CONFLICT (user_id) DO UPDATE SET
		token_hash = EXCLUDED.token_hash,
		email = EXCLUDED.email,
		created_at = EXCLUDED.created_at,
		expires_at = EXCLUDED.expires_at,
		consumed_at = 0`

	GetEmailVerificationTokenQuery = `
	SELECT user_id, token_hash, email, created_at, expires_at, consumed_at
	FROM email_verification_tokens
	WHERE token_hash = $1
	LIMIT 1`

	ConsumeEmailVerificationTokenStatement = `
	UPDATE email_verification_tokens SET consumed_at = $1 WHERE token_hash = $2 AND consumed_at = 0`

	CreateUserCredentialStatement = `
	INSERT INTO user_credentials (user_id, hashed_password, created_at, updated_at)
	VALUES ($1, $2, $3, 0)
	ON CONFLICT (user_id) DO NOTHING`

	GetUserCredentialQuery = `
	SELECT user_id, hashed_password, created_at, updated_at
	FROM user_credentials
	WHERE user_id = $1
	LIMIT 1`

	UpdateUserCredentialStatement = `
	UPDATE user_credentials
	SET hashed_password = $1, updated_at = $2
	WHERE user_id = $3`

	UpsertUserCredentialStatement = `
	INSERT INTO user_credentials (user_id, hashed_password, created_at, updated_at)
	VALUES ($1, $2, $3, 0)
	ON CONFLICT (user_id) DO UPDATE SET
		hashed_password = EXCLUDED.hashed_password,
		updated_at = EXCLUDED.created_at`
)
