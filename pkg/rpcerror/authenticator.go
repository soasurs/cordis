package rpcerror

const (
	AuthenticatorDomain = "authenticator.cordis"

	AuthenticatorInvalidCredentials  = "invalid_credentials"
	AuthenticatorInvalidAccessToken  = "invalid_access_token"
	AuthenticatorInvalidRefreshToken = "invalid_refresh_token"
	AuthenticatorSessionExpired      = "session_expired"
	AuthenticatorSessionRevoked      = "session_revoked"
)
