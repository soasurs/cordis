// Package ratelimit defines Gateway rate-limit policy names.
package ratelimit

import "github.com/soasurs/cordis/pkg/clientip"

const (
	PolicyUpgradeIP     = "upgrade_ip"
	PolicyIdentifyIP    = "identify_ip"
	PolicyResumeIP      = "resume_ip"
	PolicyResumeSession = "resume_session"
)

var requiredPolicies = []string{
	PolicyForFamily(PolicyUpgradeIP, clientip.FamilyIPv4),
	PolicyForFamily(PolicyUpgradeIP, clientip.FamilyIPv6),
	PolicyForFamily(PolicyIdentifyIP, clientip.FamilyIPv4),
	PolicyForFamily(PolicyIdentifyIP, clientip.FamilyIPv6),
	PolicyForFamily(PolicyResumeIP, clientip.FamilyIPv4),
	PolicyForFamily(PolicyResumeIP, clientip.FamilyIPv6),
	PolicyResumeSession,
}

// PolicyForFamily selects the configured policy for an IP source family.
func PolicyForFamily(policy string, family clientip.Family) string {
	return policy + "_" + string(family)
}

// RequiredPolicies returns every Gateway policy required at startup.
func RequiredPolicies() []string {
	return append([]string(nil), requiredPolicies...)
}
