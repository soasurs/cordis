package ratelimit

import (
	"context"
	"sync"
	"time"
)

const localSweepInterval = 1024

type localBucket struct {
	used    int64
	resetAt time.Time
}

// LocalBackend is a bounded in-process fixed-window limiter.
type LocalBackend struct {
	mu       sync.Mutex
	buckets  map[string]localBucket
	maxKeys  int
	requests uint64
	now      func() time.Time
}

// NewLocalBackend constructs an in-process limiter capped at maxKeys buckets.
func NewLocalBackend(maxKeys int) *LocalBackend {
	if maxKeys <= 0 {
		maxKeys = defaultFallbackMaxKeys
	}
	return &LocalBackend{
		buckets: make(map[string]localBucket),
		maxKeys: maxKeys,
		now:     time.Now,
	}
}

// Take consumes cost from a local fixed-window bucket.
func (l *LocalBackend) Take(ctx context.Context, key string, policy Policy, cost int64) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.requests++
	if l.requests%localSweepInterval == 0 {
		l.sweep(now)
	}

	bucket, ok := l.buckets[key]
	if ok && !now.Before(bucket.resetAt) {
		delete(l.buckets, key)
		ok = false
	}
	if !ok {
		if len(l.buckets) >= l.maxKeys {
			l.sweep(now)
		}
		if len(l.buckets) >= l.maxKeys {
			return Decision{Limit: policy.Limit, RetryAfter: policy.Window}, nil
		}
		bucket = localBucket{resetAt: now.Add(policy.Window)}
	}

	bucket.used += cost
	l.buckets[key] = bucket
	remaining := max(policy.Limit-bucket.used, 0)
	allowed := bucket.used <= policy.Limit
	decision := Decision{
		Allowed:   allowed,
		Limit:     policy.Limit,
		Remaining: remaining,
	}
	if !allowed {
		decision.RetryAfter = max(bucket.resetAt.Sub(now), time.Millisecond)
	}
	return decision, nil
}

func (l *LocalBackend) sweep(now time.Time) {
	for key, bucket := range l.buckets {
		if !now.Before(bucket.resetAt) {
			delete(l.buckets, key)
		}
	}
}
