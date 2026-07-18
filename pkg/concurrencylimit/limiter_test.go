package concurrencylimit

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewValidatesConfiguration(t *testing.T) {
	_, err := New("", 1)
	require.EqualError(t, err, "concurrency limiter name is required")
	_, err = New("argon2", 0)
	require.EqualError(t, err, "concurrency limiter capacity must be positive")
}

func TestLimiterTracksWeightAndReleaseIsIdempotent(t *testing.T) {
	limiter, err := New("test_weight", 3)
	require.NoError(t, err)

	release, err := limiter.Acquire(t.Context(), 2)
	require.NoError(t, err)
	require.Equal(t, int64(2), limiter.InUse())
	require.Equal(t, int64(3), limiter.Capacity())

	_, ok := limiter.TryAcquire(2)
	require.False(t, ok)
	release()
	release()
	require.Zero(t, limiter.InUse())

	release, ok = limiter.TryAcquire(3)
	require.True(t, ok)
	require.Equal(t, int64(3), limiter.InUse())
	release()
	require.Zero(t, limiter.InUse())
}

func TestLimiterAcquireHonorsContext(t *testing.T) {
	limiter, err := New("test_context", 1)
	require.NoError(t, err)
	release, err := limiter.Acquire(t.Context(), 1)
	require.NoError(t, err)
	defer release()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = limiter.Acquire(ctx, 1)
	require.ErrorIs(t, err, context.Canceled)
}

func TestLimiterRejectsInvalidWeight(t *testing.T) {
	limiter, err := New("test_invalid_weight", 2)
	require.NoError(t, err)

	_, err = limiter.Acquire(t.Context(), 0)
	require.EqualError(t, err, "concurrency limiter weight must be positive")
	_, err = limiter.Acquire(t.Context(), 3)
	require.EqualError(t, err, "concurrency limiter weight exceeds capacity")
	_, ok := limiter.TryAcquire(3)
	require.False(t, ok)
}
