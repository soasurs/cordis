package outbox

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShardIDIsStableAndBounded(t *testing.T) {
	const shardCount = 64
	first := ShardID("channel-42", shardCount)
	require.GreaterOrEqual(t, first, 0)
	require.Less(t, first, shardCount)
	require.Equal(t, first, ShardID("channel-42", shardCount))
}

func TestQuoteIdentEscapesDoubleQuotes(t *testing.T) {
	require.Equal(t, `"events"`, quoteIdent("events"))
	require.Equal(t, `"out""box"`, quoteIdent(`out"box`))
	require.Panics(t, func() { quoteIdent("") })
}
