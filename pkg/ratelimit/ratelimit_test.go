package ratelimit

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

type fakeBackend struct {
	mu       sync.Mutex
	calls    int
	keys     []string
	decision Decision
	err      error
}

func (b *fakeBackend) Take(_ context.Context, key string, _ Policy, _ int64) (Decision, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	b.keys = append(b.keys, key)
	return b.decision, b.err
}

func (b *fakeBackend) snapshot() (int, []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls, append([]string(nil), b.keys...)
}

type fakeScriptRunner struct {
	value any
	err   error
}

func (r *fakeScriptRunner) ScriptRunCtx(
	_ context.Context,
	_ *redis.Script,
	_ []string,
	_ ...any,
) (any, error) {
	return r.value, r.err
}

func TestNewManagerValidatesPolicies(t *testing.T) {
	tests := []struct {
		name     string
		policies map[string]Policy
	}{
		{name: "missing"},
		{name: "invalid name", policies: map[string]Policy{"Invalid": {Limit: 1, Window: time.Second}}},
		{name: "invalid limit", policies: map[string]Policy{"valid": {Window: time.Second}}},
		{name: "invalid window", policies: map[string]Policy{"valid": {Limit: 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewManager(new(fakeBackend), tt.policies, Options{})
			require.Error(t, err)
		})
	}
}

func TestManagerUsesHashedPrimaryKey(t *testing.T) {
	backend := &fakeBackend{decision: Decision{Allowed: true, Limit: 10, Remaining: 9}}
	manager, err := NewManager(backend, map[string]Policy{
		"api": {Limit: 10, Window: time.Minute},
	}, Options{KeyPrefix: "test:"})
	require.NoError(t, err)

	decision, err := manager.Take(t.Context(), "api", "sensitive@example.com", 1)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	calls, keys := backend.snapshot()
	require.Equal(t, 1, calls)
	require.Len(t, keys, 1)
	require.True(t, strings.HasPrefix(keys[0], "test:api:{"))
	require.NotContains(t, keys[0], "sensitive@example.com")
}

func TestManagerFallsBackAndSkipsFailedBackend(t *testing.T) {
	backend := &fakeBackend{err: errors.New("redis unavailable")}
	manager, err := NewManager(backend, map[string]Policy{
		"api": {Limit: 1, Window: time.Minute},
	}, Options{FallbackRetryInterval: time.Minute})
	require.NoError(t, err)

	first, err := manager.Take(t.Context(), "api", "client", 1)
	require.NoError(t, err)
	require.True(t, first.Allowed)
	require.True(t, first.Fallback)

	second, err := manager.Take(t.Context(), "api", "client", 1)
	require.NoError(t, err)
	require.False(t, second.Allowed)
	require.True(t, second.Fallback)
	require.Positive(t, second.RetryAfter)

	calls, _ := backend.snapshot()
	require.Equal(t, 1, calls)
}

func TestManagerDoesNotFallbackOnRequestCancellation(t *testing.T) {
	backend := &fakeBackend{err: context.Canceled}
	manager, err := NewManager(backend, map[string]Policy{
		"api": {Limit: 1, Window: time.Minute},
	}, Options{})
	require.NoError(t, err)

	_, err = manager.Take(t.Context(), "api", "client", 1)
	require.ErrorIs(t, err, context.Canceled)
	calls, _ := backend.snapshot()
	require.Equal(t, 1, calls)
}

func TestManagerRejectsInvalidRequests(t *testing.T) {
	backend := &fakeBackend{}
	manager, err := NewManager(backend, map[string]Policy{
		"api": {Limit: 2, Window: time.Minute},
	}, Options{})
	require.NoError(t, err)

	_, err = manager.Take(t.Context(), "missing", "client", 1)
	require.ErrorIs(t, err, ErrUnknownPolicy)
	_, err = manager.Take(t.Context(), "api", "client", 0)
	require.EqualError(t, err, "rate limit cost must be positive")

	decision, err := manager.Take(t.Context(), "api", "client", 3)
	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.Equal(t, time.Minute, decision.RetryAfter)
	calls, _ := backend.snapshot()
	require.Zero(t, calls)
}

func TestLocalBackendResetsAndCapsBuckets(t *testing.T) {
	now := time.Unix(100, 0)
	backend := NewLocalBackend(1)
	backend.now = func() time.Time { return now }
	policy := Policy{Limit: 2, Window: time.Minute}

	decision, err := backend.Take(t.Context(), "one", policy, 2)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.Zero(t, decision.Remaining)

	decision, err = backend.Take(t.Context(), "one", policy, 1)
	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.Equal(t, time.Minute, decision.RetryAfter)

	decision, err = backend.Take(t.Context(), "two", policy, 1)
	require.NoError(t, err)
	require.False(t, decision.Allowed)

	now = now.Add(time.Minute)
	decision, err = backend.Take(t.Context(), "two", policy, 1)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
}

func TestLocalBackendHonorsContext(t *testing.T) {
	backend := NewLocalBackend(1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := backend.Take(ctx, "one", Policy{Limit: 1, Window: time.Minute}, 1)
	require.ErrorIs(t, err, context.Canceled)
}

func TestRedisBackendDecodesDecision(t *testing.T) {
	backend := &RedisBackend{store: &fakeScriptRunner{
		value: []any{int64(0), int64(0), int64(1500)},
	}}
	decision, err := backend.Take(t.Context(), "key", Policy{Limit: 5, Window: time.Minute}, 1)
	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.Equal(t, int64(5), decision.Limit)
	require.Equal(t, 1500*time.Millisecond, decision.RetryAfter)
}
