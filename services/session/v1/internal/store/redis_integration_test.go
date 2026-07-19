//go:build integration

package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/soasurs/cordis/internal/testkit"
)

func TestRedisStorePersistsOwnersAndRoutes(t *testing.T) {
	container := testkit.StartRedis(t)
	rds, err := redis.NewRedis(redis.RedisConf{Host: container.Address, Type: redis.NodeType})
	require.NoError(t, err)

	store := NewRedisStore(rds)
	ctx := t.Context()
	require.NoError(t, store.SetOwner(ctx, Owner{
		SessionID:  "session-1",
		NodeID:     "node-1",
		Generation: "generation-1",
	}, time.Minute))

	owner, err := rds.HmgetCtx(ctx, ownerKey("session-1"), "node_id", "generation", "expires_at")
	require.NoError(t, err)
	require.Equal(t, []string{"node-1", "generation-1", owner[2]}, owner)
	require.NotEmpty(t, owner[2])
	require.NoError(t, store.SetOwners(ctx, []Owner{
		{SessionID: "session-2", NodeID: "node-1", Generation: "generation-1"},
		{SessionID: "session-3", NodeID: "node-1", Generation: "generation-1"},
	}, time.Minute))
	for _, sessionID := range []string{"session-2", "session-3"} {
		values, err := rds.HmgetCtx(ctx, ownerKey(sessionID), "node_id", "generation")
		require.NoError(t, err)
		require.Equal(t, []string{"node-1", "generation-1"}, values)
	}

	routes := []Route{{Kind: RouteUser, ID: 1001}, {Kind: RouteGuild, ID: 2001}}
	require.NoError(t, store.RefreshRoutes(ctx, "node-1", "generation-1", routes, time.Minute))
	members, err := rds.ZrangeCtx(ctx, routeKey(RouteGuild, 2001), 0, -1)
	require.NoError(t, err)
	require.Equal(t, []string{"node-1\x1fgeneration-1"}, members)

	require.NoError(t, store.DetachRoutes(ctx, "node-1", "generation-1", routes))
	members, err = rds.ZrangeCtx(ctx, routeKey(RouteGuild, 2001), 0, -1)
	require.NoError(t, err)
	require.Empty(t, members)
}

func TestRedisStoreDeleteOwner(t *testing.T) {
	container := testkit.StartRedis(t)
	rds, err := redis.NewRedis(redis.RedisConf{Host: container.Address, Type: redis.NodeType})
	require.NoError(t, err)

	store := NewRedisStore(rds)
	ctx := t.Context()
	owner := Owner{SessionID: "session-1", NodeID: "node-1", Generation: "generation-1"}
	require.NoError(t, store.SetOwner(ctx, owner, time.Minute))

	require.NoError(t, store.DeleteOwner(ctx, "session-1", "node-1", "generation-stale"))
	values, err := rds.HmgetCtx(ctx, ownerKey("session-1"), "node_id")
	require.NoError(t, err)
	require.Equal(t, []string{"node-1"}, values)

	require.NoError(t, store.DeleteOwner(ctx, "session-1", "node-2", "generation-1"))
	values, err = rds.HmgetCtx(ctx, ownerKey("session-1"), "node_id")
	require.NoError(t, err)
	require.Equal(t, []string{"node-1"}, values)

	require.NoError(t, store.DeleteOwner(ctx, "session-1", "node-1", "generation-1"))
	exists, err := rds.ExistsCtx(ctx, ownerKey("session-1"))
	require.NoError(t, err)
	require.False(t, exists)

	require.NoError(t, store.DeleteOwner(ctx, "session-missing", "node-1", "generation-1"))
}
