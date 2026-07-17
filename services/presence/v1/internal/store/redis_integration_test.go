//go:build integration

package store

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/soasurs/cordis/internal/testkit"
)

func TestRedisStoreGatewayRoutes(t *testing.T) {
	rds := newIntegrationRedis(t)
	store := NewRedisStore(rds, time.Minute, time.Minute, time.Minute)
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
	store := NewRedisStore(rds, time.Minute, time.Minute, time.Minute)
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
	store := NewRedisStore(rds, time.Minute, time.Minute, time.Minute)
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

func TestRedisStoreUserPresenceLifecycle(t *testing.T) {
	rds := newIntegrationRedis(t)
	store := NewRedisStore(rds, time.Minute, time.Minute, time.Minute)
	ctx := t.Context()
	userID := time.Now().UnixNano()
	sessionA := "sess-a-" + time.Now().Format("150405.000000000")
	sessionB := "sess-b-" + time.Now().Format("150405.000000000")
	t.Cleanup(func() {
		_, _ = rds.DelCtx(ctx, userSessionsKey(userID), userSessionKey(sessionA), userSessionKey(sessionB))
	})

	presence, err := store.UpsertUserSession(ctx, UserSession{
		UserID:      userID,
		SessionID:   sessionA,
		GatewayID:   "gw-a",
		Generation:  "gen-1",
		DeviceType:  "desktop",
		Status:      PresenceStatusOnline,
		ClientState: ClientStateForeground,
	})
	require.NoError(t, err)
	require.Equal(t, PresenceStatusOnline, presence.Status)
	require.Len(t, presence.Sessions, 1)
	require.Equal(t, sessionA, presence.Sessions[0].SessionID)

	presence, err = store.UpsertUserSession(ctx, UserSession{
		UserID:      userID,
		SessionID:   sessionB,
		GatewayID:   "gw-b",
		Generation:  "gen-1",
		DeviceType:  "mobile",
		Status:      PresenceStatusDND,
		ClientState: ClientStateBackground,
	})
	require.NoError(t, err)
	require.Equal(t, PresenceStatusDND, presence.Status)
	require.Len(t, presence.Sessions, 2)

	resolved, err := store.ResolveUsersPresence(ctx, []int64{userID, userID + 1})
	require.NoError(t, err)
	require.Len(t, resolved, 2)
	require.Equal(t, PresenceStatusDND, resolved[0].Status)
	require.Equal(t, PresenceStatusOffline, resolved[1].Status)

	err = store.RemoveUserSession(ctx, userID, sessionB)
	require.NoError(t, err)
	resolved, err = store.ResolveUsersPresence(ctx, []int64{userID})
	require.NoError(t, err)
	require.Equal(t, PresenceStatusOnline, resolved[0].Status)
	require.Len(t, resolved[0].Sessions, 1)
	require.Equal(t, sessionA, resolved[0].Sessions[0].SessionID)

	presence, err = store.UpdateUserSession(ctx, UserSession{
		UserID:      userID,
		SessionID:   sessionA,
		Status:      PresenceStatusIdle,
		ClientState: ClientStateBackground,
	})
	require.NoError(t, err)
	require.Equal(t, PresenceStatusIdle, presence.Status)
	require.Len(t, presence.Sessions, 1)
	require.Equal(t, "gw-a", presence.Sessions[0].GatewayID)
	require.Equal(t, "desktop", presence.Sessions[0].DeviceType)
	require.Equal(t, ClientStateBackground, presence.Sessions[0].ClientState)
}

func TestRedisStoreInvisiblePresenceResolvesOffline(t *testing.T) {
	rds := newIntegrationRedis(t)
	store := NewRedisStore(rds, time.Minute, time.Minute, time.Minute)
	ctx := t.Context()
	userID := time.Now().UnixNano()
	sessionID := "sess-invisible-" + time.Now().Format("150405.000000000")
	t.Cleanup(func() {
		_, _ = rds.DelCtx(ctx, userSessionsKey(userID), userSessionKey(sessionID))
	})

	_, err := store.UpsertUserSession(ctx, UserSession{
		UserID:      userID,
		SessionID:   sessionID,
		GatewayID:   "gw-a",
		Generation:  "gen-1",
		Status:      PresenceStatusInvisible,
		ClientState: ClientStateForeground,
	})
	require.NoError(t, err)

	resolved, err := store.ResolveUsersPresence(ctx, []int64{userID})
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	require.Equal(t, PresenceStatusOffline, resolved[0].Status)
	require.Empty(t, resolved[0].Sessions)
}

func TestRedisStoreFiltersExpiredUserSessions(t *testing.T) {
	rds := newIntegrationRedis(t)
	store := NewRedisStore(rds, time.Minute, time.Minute, time.Minute)
	ctx := t.Context()
	userID := time.Now().UnixNano()
	sessionID := "sess-expired-" + time.Now().Format("150405.000000000")
	t.Cleanup(func() {
		_, _ = rds.DelCtx(ctx, userSessionsKey(userID), userSessionKey(sessionID))
	})

	base := time.UnixMilli(1000)
	store.now = func() time.Time { return base }
	_, err := store.UpsertUserSession(ctx, UserSession{
		UserID:      userID,
		SessionID:   sessionID,
		GatewayID:   "gw-a",
		Generation:  "gen-1",
		Status:      PresenceStatusOnline,
		ClientState: ClientStateForeground,
	})
	require.NoError(t, err)

	store.now = func() time.Time { return base.Add(2 * time.Minute) }
	resolved, err := store.ResolveUsersPresence(ctx, []int64{userID})
	require.NoError(t, err)
	require.Equal(t, PresenceStatusOffline, resolved[0].Status)
	require.Empty(t, resolved[0].Sessions)
}

func newIntegrationRedis(t *testing.T) *redis.Redis {
	t.Helper()
	addr := os.Getenv("CORDIS_TEST_REDIS_ADDR")
	if addr == "" {
		addr = testkit.StartRedis(t).Address
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
