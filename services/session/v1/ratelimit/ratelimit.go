// Package ratelimit defines Session rate-limit policy names.
package ratelimit

const (
	PolicyIdentifyUser        = "identify_user"
	PolicyIdentifyAuthSession = "identify_auth_session"
)

var requiredPolicies = []string{
	PolicyIdentifyUser,
	PolicyIdentifyAuthSession,
}

// RequiredPolicies returns every Session policy required at startup.
func RequiredPolicies() []string {
	return append([]string(nil), requiredPolicies...)
}
