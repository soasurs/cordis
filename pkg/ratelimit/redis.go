package ratelimit

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

//go:embed fixed_window.lua
var fixedWindowLua string

var fixedWindowScript = redis.NewScript(fixedWindowLua)

type scriptRunner interface {
	ScriptRunCtx(ctx context.Context, script *redis.Script, keys []string, args ...any) (any, error)
}

// RedisBackend stores fixed-window counters in Redis.
type RedisBackend struct {
	store scriptRunner
}

// NewRedisBackend constructs a Redis-backed fixed-window limiter.
func NewRedisBackend(store *redis.Redis) *RedisBackend {
	return &RedisBackend{store: store}
}

// Take atomically consumes cost from a Redis fixed-window bucket.
func (r *RedisBackend) Take(ctx context.Context, key string, policy Policy, cost int64) (Decision, error) {
	if r == nil || r.store == nil {
		return Decision{}, errors.New("redis rate limit store is required")
	}
	windowMillis := max(policy.Window.Milliseconds(), int64(1))
	value, err := r.store.ScriptRunCtx(ctx, fixedWindowScript, []string{key}, cost, policy.Limit, windowMillis)
	if err != nil {
		return Decision{}, err
	}
	values, ok := value.([]any)
	if !ok || len(values) != 3 {
		return Decision{}, fmt.Errorf("decode redis rate limit result: %T", value)
	}
	allowed, ok := redisInt64(values[0])
	if !ok {
		return Decision{}, fmt.Errorf("decode redis rate limit allowed: %T", values[0])
	}
	remaining, ok := redisInt64(values[1])
	if !ok {
		return Decision{}, fmt.Errorf("decode redis rate limit remaining: %T", values[1])
	}
	ttlMillis, ok := redisInt64(values[2])
	if !ok {
		return Decision{}, fmt.Errorf("decode redis rate limit ttl: %T", values[2])
	}

	decision := Decision{
		Allowed:   allowed == 1,
		Limit:     policy.Limit,
		Remaining: remaining,
	}
	if !decision.Allowed {
		decision.RetryAfter = max(time.Duration(ttlMillis)*time.Millisecond, time.Millisecond)
	}
	return decision, nil
}

func redisInt64(value any) (int64, bool) {
	switch value := value.(type) {
	case int64:
		return value, true
	case int:
		return int64(value), true
	default:
		return 0, false
	}
}
