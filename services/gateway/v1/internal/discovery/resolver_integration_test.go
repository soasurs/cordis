//go:build integration

package discovery

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/sessionregistry"
)

// TestSessionResolverIntegration shares Redis and etcd containers across all
// subtests; each subtest uses its own etcd prefix and session IDs.
func TestSessionResolverIntegration(t *testing.T) {
	redisContainer := testkit.StartRedis(t)
	etcd := testkit.StartEtcd(t)
	rds, err := redis.NewRedis(redis.RedisConf{Host: redisContainer.Address, Type: redis.NodeType})
	require.NoError(t, err)
	env := &resolverEnv{rds: rds, etcdHosts: []string{etcd.Address}}

	t.Run("resolves ready node and owned session", func(t *testing.T) { testResolveHappyPath(t, env) })
	t.Run("identify fails without ready nodes", func(t *testing.T) { testResolveNodeWithoutNodes(t, env) })
	t.Run("identify excludes draining nodes", func(t *testing.T) { testResolveNodeExcludesDraining(t, env) })
	t.Run("resume fails without owner", func(t *testing.T) { testResolveSessionWithoutOwner(t, env) })
	t.Run("resume fails with expired owner", func(t *testing.T) { testResolveSessionExpiredOwner(t, env) })
	t.Run("resume fails after node crash", func(t *testing.T) { testResolveSessionAfterNodeCrash(t, env) })
	t.Run("resume fails with stale generation", func(t *testing.T) { testResolveSessionStaleGeneration(t, env) })
	t.Run("resume fails on draining node", func(t *testing.T) { testResolveSessionDrainingNode(t, env) })
}

func testResolveHappyPath(t *testing.T, env *resolverEnv) {
	const address = "127.0.0.1:3006"
	h := newResolverHarness(t, env)
	h.registerNode(t, "session-node", "generation-1", address, sessionregistry.StatusReady)
	h.setOwner(t, "session-1", "session-node", "generation-1", time.Now().Add(time.Minute).UnixMilli())

	resolvedNode, err := h.resolver.ResolveNode(t.Context())
	require.NoError(t, err)
	require.Equal(t, address, resolvedNode)

	resolvedSession, err := h.resolver.ResolveSession(t.Context(), "session-1")
	require.NoError(t, err)
	require.Equal(t, address, resolvedSession)
}

func testResolveNodeWithoutNodes(t *testing.T, env *resolverEnv) {
	h := newResolverHarness(t, env)
	_, err := h.resolver.ResolveNode(t.Context())
	require.ErrorContains(t, err, "ready session node not found")
}

func testResolveNodeExcludesDraining(t *testing.T, env *resolverEnv) {
	h := newResolverHarness(t, env)
	h.registerNode(t, "session-node", "generation-1", "127.0.0.1:3006", sessionregistry.StatusDraining)
	_, err := h.resolver.ResolveNode(t.Context())
	require.ErrorContains(t, err, "ready session node not found")
}

func testResolveSessionWithoutOwner(t *testing.T, env *resolverEnv) {
	h := newResolverHarness(t, env)
	h.registerNode(t, "session-node", "generation-1", "127.0.0.1:3006", sessionregistry.StatusReady)
	_, err := h.resolver.ResolveSession(t.Context(), "session-unknown")
	require.ErrorContains(t, err, "session owner not found")
}

func testResolveSessionExpiredOwner(t *testing.T, env *resolverEnv) {
	h := newResolverHarness(t, env)
	h.registerNode(t, "session-node", "generation-1", "127.0.0.1:3006", sessionregistry.StatusReady)
	h.setOwner(t, "session-expired", "session-node", "generation-1", time.Now().Add(-time.Second).UnixMilli())
	_, err := h.resolver.ResolveSession(t.Context(), "session-expired")
	require.ErrorContains(t, err, "session owner not found")
}

func testResolveSessionAfterNodeCrash(t *testing.T, env *resolverEnv) {
	h := newResolverHarness(t, env)
	registry := h.registerNode(t, "session-node", "generation-1", "127.0.0.1:3006", sessionregistry.StatusReady)
	h.setOwner(t, "session-crash", "session-node", "generation-1", time.Now().Add(time.Minute).UnixMilli())

	resolved, err := h.resolver.ResolveSession(t.Context(), "session-crash")
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:3006", resolved)

	require.NoError(t, registry.Close())
	_, err = h.resolver.ResolveSession(t.Context(), "session-crash")
	require.ErrorIs(t, err, sessionregistry.ErrNodeNotFound)
}

func testResolveSessionStaleGeneration(t *testing.T, env *resolverEnv) {
	h := newResolverHarness(t, env)
	h.registerNode(t, "session-node", "generation-2", "127.0.0.1:3006", sessionregistry.StatusReady)
	h.setOwner(t, "session-stale", "session-node", "generation-1", time.Now().Add(time.Minute).UnixMilli())
	_, err := h.resolver.ResolveSession(t.Context(), "session-stale")
	require.ErrorIs(t, err, sessionregistry.ErrNodeNotFound)
}

func testResolveSessionDrainingNode(t *testing.T, env *resolverEnv) {
	h := newResolverHarness(t, env)
	h.registerNode(t, "session-node", "generation-1", "127.0.0.1:3006", sessionregistry.StatusDraining)
	h.setOwner(t, "session-draining", "session-node", "generation-1", time.Now().Add(time.Minute).UnixMilli())
	_, err := h.resolver.ResolveSession(t.Context(), "session-draining")
	require.ErrorIs(t, err, sessionregistry.ErrNodeNotReady)
}

type resolverEnv struct {
	rds       *redis.Redis
	etcdHosts []string
}

type resolverHarness struct {
	env      *resolverEnv
	prefix   string
	resolver *SessionResolver
}

func newResolverHarness(t *testing.T, env *resolverEnv) *resolverHarness {
	t.Helper()
	prefix := "/cordis/gateway-integration/" + strconv.FormatInt(time.Now().UnixNano(), 10)
	directory, err := sessionregistry.New(sessionregistry.Config{
		Hosts:              env.etcdHosts,
		Prefix:             prefix,
		DialTimeoutSeconds: 5,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, directory.Close()) })
	return &resolverHarness{env: env, prefix: prefix, resolver: New(env.rds, directory)}
}

// registerNode registers a node through its own registry instance, mirroring
// a session process owning its own etcd client and lease. Closing the
// returned registry revokes the lease and removes the node from the
// directory, simulating a crash.
func (h *resolverHarness) registerNode(
	t *testing.T,
	nodeID, generation, address, status string,
) *sessionregistry.EtcdDirectory {
	t.Helper()
	registry, err := sessionregistry.New(sessionregistry.Config{
		Hosts:              h.env.etcdHosts,
		Prefix:             h.prefix,
		DialTimeoutSeconds: 5,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = registry.Close() })
	require.NoError(t, registry.Register(t.Context(), sessionregistry.Node{
		ID:         nodeID,
		Generation: generation,
		RPCAddress: address,
		Status:     status,
	}, time.Minute))
	return registry
}

func (h *resolverHarness) setOwner(t *testing.T, sessionID, nodeID, generation string, expiresAt int64) {
	t.Helper()
	key := ownerKey(sessionID)
	ctx := t.Context()
	require.NoError(t, h.env.rds.HsetCtx(ctx, key, "node_id", nodeID))
	require.NoError(t, h.env.rds.HsetCtx(ctx, key, "generation", generation))
	require.NoError(t, h.env.rds.HsetCtx(ctx, key, "expires_at", strconv.FormatInt(expiresAt, 10)))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = h.env.rds.DelCtx(cleanupCtx, key)
	})
}
