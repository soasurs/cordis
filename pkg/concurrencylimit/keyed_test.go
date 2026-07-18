package concurrencylimit

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKeyedLimiterBoundsEachKeyIndependently(t *testing.T) {
	limiter, err := NewKeyed("test_keyed", 1)
	require.NoError(t, err)

	releaseA, err := limiter.Acquire(t.Context(), "a", 1)
	require.NoError(t, err)
	releaseB, err := limiter.Acquire(t.Context(), "b", 1)
	require.NoError(t, err)
	require.Equal(t, int64(2), limiter.InUse())

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = limiter.Acquire(ctx, "a", 1)
	require.ErrorIs(t, err, context.Canceled)

	releaseA()
	releaseA()
	releaseB()
	require.Zero(t, limiter.InUse())
	require.Empty(t, limiter.entries)
}

func TestKeyedLimiterValidatesKeyAndWeight(t *testing.T) {
	limiter, err := NewKeyed("test_keyed_validation", 2)
	require.NoError(t, err)

	_, err = limiter.Acquire(t.Context(), "", 1)
	require.EqualError(t, err, "concurrency limiter key is required")
	_, err = limiter.Acquire(t.Context(), "a", 0)
	require.EqualError(t, err, "concurrency limiter weight must be positive")
	_, err = limiter.Acquire(t.Context(), "a", 3)
	require.EqualError(t, err, "concurrency limiter weight exceeds capacity")
}
