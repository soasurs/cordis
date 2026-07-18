//go:build integration

package ratelimit

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/soasurs/cordis/internal/testkit"
)

func TestRedisBackendFixedWindow(t *testing.T) {
	container := testkit.StartRedis(t)
	rds, err := redis.NewRedis(redis.RedisConf{
		Host: container.Address,
		Type: redis.NodeType,
	})
	require.NoError(t, err)
	backend := NewRedisBackend(rds)
	policy := Policy{Limit: 2, Window: time.Minute}
	key := "test:rate_limit:{" + strconv.FormatInt(time.Now().UnixNano(), 10) + "}"
	t.Cleanup(func() {
		_, _ = rds.DelCtx(context.Background(), key)
	})

	first, err := backend.Take(t.Context(), key, policy, 1)
	require.NoError(t, err)
	require.True(t, first.Allowed)
	require.Equal(t, int64(1), first.Remaining)

	second, err := backend.Take(t.Context(), key, policy, 1)
	require.NoError(t, err)
	require.True(t, second.Allowed)
	require.Zero(t, second.Remaining)

	third, err := backend.Take(t.Context(), key, policy, 1)
	require.NoError(t, err)
	require.False(t, third.Allowed)
	require.Zero(t, third.Remaining)
	require.Positive(t, third.RetryAfter)
	require.LessOrEqual(t, third.RetryAfter, time.Minute)
}
