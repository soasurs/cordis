// Package ratelimit provides distributed fixed-window request limiting with
// an in-process fallback for temporary backend failures.
package ratelimit

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"
)

const (
	defaultKeyPrefix             = "cordis:rate_limit:"
	defaultFallbackMaxKeys       = 10000
	defaultFallbackRetryInterval = time.Second
)

var policyNamePattern = regexp.MustCompile("^[a-z][a-z0-9_]{0,63}$")

// ErrUnknownPolicy indicates that a caller requested a policy that was not
// configured when the limiter was constructed.
var ErrUnknownPolicy = errors.New("unknown rate limit policy")

// Policy defines a fixed-window quota.
type Policy struct {
	// Limit is the maximum total cost allowed in one window.
	Limit int64
	// Window is measured from the first request that creates the bucket.
	Window time.Duration
}

// Decision describes the state of a rate-limit bucket after consuming cost.
type Decision struct {
	// Allowed reports whether the operation may proceed.
	Allowed bool
	// Limit is the configured policy quota.
	Limit int64
	// Remaining is the non-negative quota left in the current window.
	Remaining int64
	// RetryAfter is set for rejected decisions.
	RetryAfter time.Duration
	// Fallback reports whether the local backend made this decision.
	Fallback bool
}

// Limiter consumes quota from named policies.
type Limiter interface {
	Take(ctx context.Context, policy, key string, cost int64) (Decision, error)
}

// Backend atomically consumes quota from one fixed-window bucket.
type Backend interface {
	Take(ctx context.Context, key string, policy Policy, cost int64) (Decision, error)
}

// Options controls Redis key names and local fallback behavior.
type Options struct {
	// KeyPrefix namespaces Redis buckets owned by this limiter.
	KeyPrefix string
	// FallbackMaxKeys bounds local memory use while Redis is unavailable.
	FallbackMaxKeys int
	// FallbackRetryInterval controls how soon Redis is probed after a failure.
	FallbackRetryInterval time.Duration
}

// Manager resolves named policies and fails over from its primary backend to
// a bounded in-process limiter when the primary backend is unavailable.
type Manager struct {
	primary  Backend
	fallback Backend
	policies map[string]Policy
	prefix   string

	retryInterval time.Duration
	backendMu     sync.Mutex
	unavailableTo time.Time
	probing       bool
}

// NewManager constructs a named-policy limiter.
func NewManager(primary Backend, policies map[string]Policy, opts Options) (*Manager, error) {
	if len(policies) == 0 {
		return nil, errors.New("rate limit policies are required")
	}
	policyCopy := make(map[string]Policy, len(policies))
	for name, policy := range policies {
		if !policyNamePattern.MatchString(name) {
			return nil, fmt.Errorf("invalid rate limit policy name %q", name)
		}
		if policy.Limit <= 0 {
			return nil, fmt.Errorf("rate limit policy %q limit must be positive", name)
		}
		if policy.Window <= 0 {
			return nil, fmt.Errorf("rate limit policy %q window must be positive", name)
		}
		policyCopy[name] = policy
	}

	prefix := opts.KeyPrefix
	if prefix == "" {
		prefix = defaultKeyPrefix
	}
	maxKeys := opts.FallbackMaxKeys
	if maxKeys <= 0 {
		maxKeys = defaultFallbackMaxKeys
	}
	retryInterval := opts.FallbackRetryInterval
	if retryInterval <= 0 {
		retryInterval = defaultFallbackRetryInterval
	}

	return &Manager{
		primary:       primary,
		fallback:      NewLocalBackend(maxKeys),
		policies:      policyCopy,
		prefix:        prefix,
		retryInterval: retryInterval,
	}, nil
}

// Take consumes cost from the policy bucket identified by key.
func (m *Manager) Take(ctx context.Context, policyName, key string, cost int64) (Decision, error) {
	policy, ok := m.policies[policyName]
	if !ok {
		return Decision{}, fmt.Errorf("%w: %s", ErrUnknownPolicy, policyName)
	}
	if cost <= 0 {
		return Decision{}, errors.New("rate limit cost must be positive")
	}

	bucketKey := m.bucketKey(policyName, key)
	if cost > policy.Limit {
		decision := Decision{Limit: policy.Limit, RetryAfter: policy.Window}
		recordDecision(policyName, decision)
		return decision, nil
	}

	if !m.beginPrimaryAttempt(time.Now()) {
		return m.takeFallback(ctx, policyName, bucketKey, policy, cost)
	}

	decision, err := m.primary.Take(ctx, bucketKey, policy, cost)
	if err == nil {
		m.finishPrimaryAttempt(true, time.Now())
		recordDecision(policyName, decision)
		return decision, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		m.cancelPrimaryAttempt()
		return Decision{}, err
	}

	m.finishPrimaryAttempt(false, time.Now())
	return m.takeFallback(ctx, policyName, bucketKey, policy, cost)
}

func (m *Manager) bucketKey(policy, key string) string {
	digest := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%s%s:{%x}", m.prefix, policy, digest)
}

func (m *Manager) takeFallback(
	ctx context.Context,
	policyName, key string,
	policy Policy,
	cost int64,
) (Decision, error) {
	decision, err := m.fallback.Take(ctx, key, policy, cost)
	if err != nil {
		return Decision{}, err
	}
	decision.Fallback = true
	recordDecision(policyName, decision)
	return decision, nil
}

func (m *Manager) beginPrimaryAttempt(now time.Time) bool {
	if m.primary == nil {
		return false
	}

	m.backendMu.Lock()
	defer m.backendMu.Unlock()
	if m.unavailableTo.IsZero() {
		return true
	}
	if now.Before(m.unavailableTo) || m.probing {
		return false
	}
	m.probing = true
	return true
}

func (m *Manager) finishPrimaryAttempt(success bool, now time.Time) {
	m.backendMu.Lock()
	defer m.backendMu.Unlock()
	m.probing = false
	if success {
		m.unavailableTo = time.Time{}
		return
	}
	m.unavailableTo = now.Add(m.retryInterval)
}

func (m *Manager) cancelPrimaryAttempt() {
	m.backendMu.Lock()
	m.probing = false
	m.backendMu.Unlock()
}
