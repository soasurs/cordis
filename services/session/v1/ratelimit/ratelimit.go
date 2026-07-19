// Package ratelimit defines Session rate-limit policy names.
package ratelimit

const (
	PolicyIdentifyUser        = "identify_user"
	PolicyIdentifyAuthSession = "identify_auth_session"
	PolicyPresenceUser        = "presence_user"
)

var requiredPolicies = []string{
	PolicyIdentifyUser,
	PolicyIdentifyAuthSession,
	PolicyPresenceUser,
}

// RequiredPolicies returns every Session policy required at startup.
func RequiredPolicies() []string {
	return append([]string(nil), requiredPolicies...)
}
