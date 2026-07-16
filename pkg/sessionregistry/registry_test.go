package sessionregistry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfigDefaults(t *testing.T) {
	var cfg Config
	require.Equal(t, defaultPrefix, cfg.KeyPrefix())
	require.Equal(t, defaultDialTimeout, cfg.DialTimeout())
}

func TestDecodeNode(t *testing.T) {
	node, err := decodeNode([]byte(`{"id":"session-a","generation":"gen-a","rpc_address":"127.0.0.1:3006","status":"ready"}`))
	require.NoError(t, err)
	require.Equal(t, "session-a", node.ID)
	require.Equal(t, StatusReady, node.Status)
}

func TestValidateNode(t *testing.T) {
	require.Error(t, validateNode(Node{}))
	require.Error(t, validateNode(Node{
		ID: "../session-a", Generation: "gen-a", RPCAddress: "127.0.0.1:3006", Status: StatusReady,
	}))
	require.NoError(t, validateNode(Node{
		ID: "session-a", Generation: "gen-a", RPCAddress: "127.0.0.1:3006", Status: StatusDraining,
	}))
	require.Equal(t, 5*time.Second, Config{}.DialTimeout())
}
