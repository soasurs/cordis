//go:build integration

package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/soasurs/cordis/internal/testkit"
)

func TestRedisStoreUserPresenceLifecycle(t *testing.T) {
	rds := newIntegrationRedis(t)
	store := NewRedisStore(rds, time.Minute, time.Second)
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
	store := NewRedisStore(rds, time.Minute, time.Second)
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
	store := NewRedisStore(rds, time.Minute, time.Second)
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

func TestRedisStoreSerializesUserMutations(t *testing.T) {
	rds := newIntegrationRedis(t)
	storeA := NewRedisStore(rds, time.Minute, time.Second)
	storeB := NewRedisStore(rds, time.Minute, time.Second)
	ctx := t.Context()
	userID := time.Now().UnixNano()
	t.Cleanup(func() {
		_, _ = rds.DelCtx(ctx, userMutationLockKey(userID))
	})

	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- storeA.WithUserMutation(ctx, userID, func(context.Context) error {
			close(firstEntered)
			<-releaseFirst
			return nil
		})
	}()
	select {
	case <-firstEntered:
	case <-time.After(time.Second):
		t.Fatal("first mutation did not acquire the lock")
	}

	contendingCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	err := storeB.WithUserMutation(contendingCtx, userID, func(context.Context) error {
		return errors.New("second mutation entered while the first held the lock")
	})
	cancel()
	require.ErrorIs(t, err, context.DeadlineExceeded)

	close(releaseFirst)
	require.NoError(t, <-firstDone)
	require.NoError(t, storeB.WithUserMutation(ctx, userID, func(context.Context) error { return nil }))

	mutationErr := errors.New("mutation failed")
	require.ErrorIs(t, storeA.WithUserMutation(ctx, userID, func(context.Context) error {
		return mutationErr
	}), mutationErr)
	require.NoError(t, storeB.WithUserMutation(ctx, userID, func(context.Context) error { return nil }))
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
