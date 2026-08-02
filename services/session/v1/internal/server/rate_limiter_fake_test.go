package server

import (
	"context"

	coreratelimit "github.com/soasurs/cordis/pkg/ratelimit"
)

type sessionRateCall struct {
	policy string
	key    string
	cost   int64
}

type sessionFakeRateLimiter struct {
	calls     []sessionRateCall
	decisions map[string]coreratelimit.Decision
}

func (l *sessionFakeRateLimiter) Take(_ context.Context, policy, key string, cost int64) (coreratelimit.Decision, error) {
	l.calls = append(l.calls, sessionRateCall{policy: policy, key: key, cost: cost})
	if decision, ok := l.decisions[policy]; ok {
		return decision, nil
	}
	return coreratelimit.Decision{Allowed: true}, nil
}
