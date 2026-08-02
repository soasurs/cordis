package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/pkg/sessionregistry"
)

func TestRegisterNodeUsesSessionRegistry(t *testing.T) {
	registry := &fakeRegistry{}
	server := newTestServerWithRegistry(registry)

	err := server.registerNode(t.Context(), sessionregistry.StatusReady)
	require.NoError(t, err)
	require.Equal(t, sessionregistry.Node{
		ID:         "session-test",
		Generation: server.generation,
		RPCAddress: "127.0.0.1:3006",
		Status:     sessionregistry.StatusReady,
	}, registry.node)
	require.Equal(t, 30*time.Second, registry.ttl)
}
