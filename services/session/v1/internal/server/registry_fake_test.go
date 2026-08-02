package server

import (
	"context"
	"time"

	"github.com/soasurs/cordis/pkg/sessionregistry"
)

type fakeRegistry struct {
	node               sessionregistry.Node
	ttl                time.Duration
	resolveNode        sessionregistry.Node
	resolveErr         error
	resolvedNodeID     string
	resolvedGeneration string
}

func (r *fakeRegistry) Register(_ context.Context, node sessionregistry.Node, ttl time.Duration) error {
	r.node = node
	r.ttl = ttl
	return nil
}

func (*fakeRegistry) Ready(context.Context) ([]sessionregistry.Node, error) { return nil, nil }

func (r *fakeRegistry) Resolve(_ context.Context, nodeID, generation string) (sessionregistry.Node, error) {
	r.resolvedNodeID = nodeID
	r.resolvedGeneration = generation
	if r.resolveErr != nil {
		return sessionregistry.Node{}, r.resolveErr
	}
	if r.resolveNode.ID != "" {
		return r.resolveNode, nil
	}
	return sessionregistry.Node{}, sessionregistry.ErrNodeNotFound
}

func (*fakeRegistry) Close() error { return nil }
