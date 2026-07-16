package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/pkg/sessionregistry"
)

func TestResolveNodeUsesReadySessionRegistry(t *testing.T) {
	resolver := New(nil, fakeDirectory{
		nodes: []sessionregistry.Node{{
			ID:         "session-a",
			Generation: "gen-a",
			RPCAddress: "127.0.0.1:3006",
			Status:     sessionregistry.StatusReady,
		}},
	})

	address, err := resolver.ResolveNode(t.Context())
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:3006", address)
}

func TestResolveNodeReturnsRegistryError(t *testing.T) {
	resolver := New(nil, fakeDirectory{err: context.DeadlineExceeded})

	_, err := resolver.ResolveNode(t.Context())
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

type fakeDirectory struct {
	nodes []sessionregistry.Node
	err   error
}

func (f fakeDirectory) Register(context.Context, sessionregistry.Node, time.Duration) error {
	return nil
}

func (f fakeDirectory) Ready(context.Context) ([]sessionregistry.Node, error) {
	return f.nodes, f.err
}

func (f fakeDirectory) Resolve(context.Context, string, string) (sessionregistry.Node, error) {
	return sessionregistry.Node{}, sessionregistry.ErrNodeNotFound
}

func (f fakeDirectory) Close() error {
	return nil
}
