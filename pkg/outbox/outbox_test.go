package outbox

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPartitionForKey(t *testing.T) {
	key := []byte("channel-123")
	first := PartitionForKey(key, DefaultPartitionCount)
	require.GreaterOrEqual(t, first, 0)
	require.Less(t, first, DefaultPartitionCount)

	second := PartitionForKey(key, DefaultPartitionCount)
	require.Equal(t, first, second, "PartitionForKey() should be deterministic")

	require.Equal(t, 2, PartitionForKey([]byte("130"), DefaultPartitionCount))
}
