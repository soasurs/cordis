package socketlimit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManagerLimitsPendingHandshakesPerScope(t *testing.T) {
	manager := NewManager()
	first, allowed := manager.Acquire("ipv4:198.51.100.7/32", 10, 10, 1)
	require.True(t, allowed)
	_, allowed = manager.Acquire("ipv4:198.51.100.7/32", 10, 10, 1)
	require.False(t, allowed)

	first.MarkReady()
	second, allowed := manager.Acquire("ipv4:198.51.100.7/32", 10, 10, 1)
	require.True(t, allowed)
	first.Release()
	second.Release()
}

func TestManagerLimitsTotalConnectionsAfterHandshake(t *testing.T) {
	manager := NewManager()
	first, allowed := manager.Acquire("scope-one", 1, 10, 10)
	require.True(t, allowed)
	first.MarkReady()
	_, allowed = manager.Acquire("scope-two", 1, 10, 10)
	require.False(t, allowed)

	first.Release()
	second, allowed := manager.Acquire("scope-two", 1, 10, 10)
	require.True(t, allowed)
	second.Release()
}

func TestManagerLimitsPendingHandshakesAcrossScopes(t *testing.T) {
	manager := NewManager()
	first, allowed := manager.Acquire("scope-one", 10, 1, 10)
	require.True(t, allowed)
	_, allowed = manager.Acquire("scope-two", 10, 1, 10)
	require.False(t, allowed)

	first.MarkReady()
	second, allowed := manager.Acquire("scope-two", 10, 1, 10)
	require.True(t, allowed)
	first.Release()
	second.Release()
}

func TestLeaseOperationsAreIdempotent(t *testing.T) {
	manager := NewManager()
	lease, allowed := manager.Acquire("scope", 1, 1, 1)
	require.True(t, allowed)
	lease.MarkReady()
	lease.MarkReady()
	lease.Release()
	lease.Release()

	replacement, allowed := manager.Acquire("scope", 1, 1, 1)
	require.True(t, allowed)
	replacement.Release()
}
