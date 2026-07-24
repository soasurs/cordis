package rpcerror

const (
	AuthenticatorDomain = "authenticator.cordis"

	AuthenticatorInvalidCredentials         = "invalid_credentials"
	AuthenticatorInvalidAccessToken         = "invalid_access_token"
	AuthenticatorInvalidRefreshToken        = "invalid_refresh_token"
	AuthenticatorSessionExpired             = "session_expired"
	AuthenticatorSessionRevoked             = "session_revoked"
	AuthenticatorInvalidTwoFactorCode       = "invalid_two_factor_code"
	AuthenticatorTwoFactorChallengeExpired  = "two_factor_challenge_expired"
	AuthenticatorTwoFactorNotEnabled        = "two_factor_not_enabled"
	AuthenticatorTwoFactorAlreadyEnabled    = "two_factor_already_enabled"
	AuthenticatorTwoFactorEnrollmentPending = "two_factor_enrollment_pending"
	AuthenticatorInvalidRegistrationInvite  = "invalid_registration_invite"
	AuthenticatorRegistrationClosed         = "registration_closed"

	AuthenticatorInvalidPasswordResetToken     = "invalid_password_reset_token"
	AuthenticatorInvalidEmailVerificationToken = "invalid_email_verification_token"
)
