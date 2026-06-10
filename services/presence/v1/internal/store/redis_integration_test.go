//go:build integration

package store

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

func TestRedisStoreGatewayRoutes(t *testing.T) {
	rds := newIntegrationRedis(t)
	store := NewRedisStore(rds, time.Minute, time.Minute)
	ctx := t.Context()
	suffix := time.Now().UnixNano()
	gatewayA := "gw-a-" + time.Now().Format("150405.000000000")
	gatewayB := "gw-b-" + time.Now().Format("150405.000000000")
	channelID := suffix
	t.Cleanup(func() {
		_, _ = rds.DelCtx(ctx, gatewayKey(gatewayA), gatewayKey(gatewayB), channelGatewaysKey(channelID))
	})

	registered, err := store.UpsertGateway(ctx, Gateway{
		GatewayID:  gatewayA,
		Generation: "gen-1",
		RPCAddr:    "10.0.0.1:3004",
	})
	require.NoError(t, err)
	require.Equal(t, gatewayA, registered.GatewayID)
	require.Equal(t, "gen-1", registered.Generation)
	require.Equal(t, "10.0.0.1:3004", registered.RPCAddr)
	require.NotZero(t, registered.ExpiresAt)

	_, err = store.UpsertGateway(ctx, Gateway{
		GatewayID:  gatewayB,
		Generation: "gen-1",
		RPCAddr:    "10.0.0.2:3004",
	})
	require.NoError(t, err)

	refreshed, err := store.RefreshChannelRoutes(ctx, gatewayA, "gen-1", []int64{channelID})
	require.NoError(t, err)
	require.Equal(t, 1, refreshed)
	refreshed, err = store.RefreshChannelRoutes(ctx, gatewayB, "gen-1", []int64{channelID})
	require.NoError(t, err)
	require.Equal(t, 1, refreshed)

	gateways, err := store.ResolveChannelGateways(ctx, channelID)
	require.NoError(t, err)
	require.ElementsMatch(t, []Gateway{
		{GatewayID: gatewayA, Generation: "gen-1", RPCAddr: "10.0.0.1:3004", ExpiresAt: gatewaysByID(gateways)[gatewayA].ExpiresAt},
		{GatewayID: gatewayB, Generation: "gen-1", RPCAddr: "10.0.0.2:3004", ExpiresAt: gatewaysByID(gateways)[gatewayB].ExpiresAt},
	}, gateways)

	err = store.DetachChannelRoute(ctx, gatewayA, "gen-1", channelID)
	require.NoError(t, err)

	gateways, err = store.ResolveChannelGateways(ctx, channelID)
	require.NoError(t, err)
	require.Len(t, gateways, 1)
	require.Equal(t, gatewayB, gateways[0].GatewayID)
}

func TestRedisStoreFiltersStaleGeneration(t *testing.T) {
	rds := newIntegrationRedis(t)
	store := NewRedisStore(rds, time.Minute, time.Minute)
	ctx := t.Context()
	gatewayID := "gw-stale-" + time.Now().Format("150405.000000000")
	channelID := time.Now().UnixNano()
	t.Cleanup(func() {
		_, _ = rds.DelCtx(ctx, gatewayKey(gatewayID), channelGatewaysKey(channelID))
	})

	_, err := store.UpsertGateway(ctx, Gateway{
		GatewayID:  gatewayID,
		Generation: "gen-1",
		RPCAddr:    "10.0.0.1:3004",
	})
	require.NoError(t, err)
	_, err = store.RefreshChannelRoutes(ctx, gatewayID, "gen-1", []int64{channelID})
	require.NoError(t, err)

	_, err = store.UpsertGateway(ctx, Gateway{
		GatewayID:  gatewayID,
		Generation: "gen-2",
		RPCAddr:    "10.0.0.1:3004",
	})
	require.NoError(t, err)

	gateways, err := store.ResolveChannelGateways(ctx, channelID)
	require.NoError(t, err)
	require.Empty(t, gateways)
}

func TestRedisStoreFiltersExpiredRoutes(t *testing.T) {
	rds := newIntegrationRedis(t)
	store := NewRedisStore(rds, time.Minute, time.Minute)
	ctx := t.Context()
	gatewayID := "gw-expired-" + time.Now().Format("150405.000000000")
	channelID := time.Now().UnixNano()
	t.Cleanup(func() {
		_, _ = rds.DelCtx(ctx, gatewayKey(gatewayID), channelGatewaysKey(channelID))
	})

	base := time.UnixMilli(1000)
	store.now = func() time.Time { return base }
	_, err := store.UpsertGateway(ctx, Gateway{
		GatewayID:  gatewayID,
		Generation: "gen-1",
		RPCAddr:    "10.0.0.1:3004",
	})
	require.NoError(t, err)
	_, err = store.RefreshChannelRoutes(ctx, gatewayID, "gen-1", []int64{channelID})
	require.NoError(t, err)

	store.now = func() time.Time { return base.Add(2 * time.Minute) }
	gateways, err := store.ResolveChannelGateways(ctx, channelID)
	require.NoError(t, err)
	require.Empty(t, gateways)
}

func newIntegrationRedis(t *testing.T) *redis.Redis {
	t.Helper()
	addr := os.Getenv("CORDIS_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("CORDIS_TEST_REDIS_ADDR is not set")
	}

	rds, err := redis.NewRedis(redis.RedisConf{
		Host:     addr,
		Type:     redis.NodeType,
		NonBlock: false,
	})
	require.NoError(t, err)
	return rds
}

func gatewaysByID(gateways []Gateway) map[string]Gateway {
	values := make(map[string]Gateway, len(gateways))
	for _, gateway := range gateways {
		values[gateway.GatewayID] = gateway
	}
	return values
}
