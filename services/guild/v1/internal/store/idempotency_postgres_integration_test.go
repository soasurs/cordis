//go:build integration

package store

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testGuildIdempotency(t *testing.T, store Store) {
	const (
		guildID, ownerID = int64(19500), int64(29500)
		actorUserID      = int64(29501)
	)
	ctx := t.Context()

	// A claim committed together with its resource is returned as the
	// existing reservation on replay.
	hash := make([]byte, 32)
	hash[0] = 0xAA
	now := time.Now().UnixMilli()
	expiresAt := now + 60_000
	err := store.Transact(ctx, func(tx Store) error {
		claim, err := tx.ClaimGuildIdempotency(ctx, ClaimGuildIdempotencyParams{
			ActorUserID: actorUserID, Operation: "guild.create", IdempotencyKey: "intent-1",
			RequestHash: hash, ResourceID: guildID, CreatedAt: now, ExpiresAt: expiresAt,
		})
		require.NoError(t, err)
		require.True(t, claim.Claimed)
		require.Equal(t, guildID, claim.ResourceID)
		require.Equal(t, hash, claim.RequestHash)
		if _, err := tx.CreateGuild(ctx, guildID, ownerID, "G", now); err != nil {
			return err
		}
		if _, err := tx.CreateGuildMember(ctx, guildID, ownerID, now); err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)

	replay, err := store.ClaimGuildIdempotency(ctx, ClaimGuildIdempotencyParams{
		ActorUserID: actorUserID, Operation: "guild.create", IdempotencyKey: "intent-1",
		RequestHash: hash, ResourceID: 99999, CreatedAt: now, ExpiresAt: expiresAt,
	})
	require.NoError(t, err)
	require.False(t, replay.Claimed)
	require.Equal(t, guildID, replay.ResourceID, "replay must return the first resource id")
	require.Equal(t, hash, replay.RequestHash)

	// Different callers sharing a key are independent.
	other, err := store.ClaimGuildIdempotency(ctx, ClaimGuildIdempotencyParams{
		ActorUserID: 77777, Operation: "guild.create", IdempotencyKey: "intent-1",
		RequestHash: hash, ResourceID: 88888, CreatedAt: now, ExpiresAt: expiresAt,
	})
	require.NoError(t, err)
	require.True(t, other.Claimed)

	// The same actor reusing a key under a different operation is independent.
	differentOperation, err := store.ClaimGuildIdempotency(ctx, ClaimGuildIdempotencyParams{
		ActorUserID: actorUserID, Operation: "guild.invite.create", IdempotencyKey: "intent-1",
		RequestHash: hash, ResourceID: 11112, CreatedAt: now, ExpiresAt: expiresAt,
	})
	require.NoError(t, err)
	require.True(t, differentOperation.Claimed)

	// A rolled-back transaction removes both the resource and the claim so
	// the key can represent a new creation intent.
	rollbackHash := make([]byte, 32)
	rollbackHash[0] = 0xBB
	rollbackNow := time.Now().UnixMilli()
	err = store.Transact(ctx, func(tx Store) error {
		claim, err := tx.ClaimGuildIdempotency(ctx, ClaimGuildIdempotencyParams{
			ActorUserID: actorUserID, Operation: "guild.role.create", IdempotencyKey: "intent-2",
			RequestHash: rollbackHash, ResourceID: 12345, CreatedAt: rollbackNow, ExpiresAt: rollbackNow + 60_000,
		})
		require.NoError(t, err)
		require.True(t, claim.Claimed)
		return errors.New("force rollback")
	})
	require.Error(t, err)
	retry, err := store.ClaimGuildIdempotency(ctx, ClaimGuildIdempotencyParams{
		ActorUserID: actorUserID, Operation: "guild.role.create", IdempotencyKey: "intent-2",
		RequestHash: rollbackHash, ResourceID: 12346, CreatedAt: rollbackNow, ExpiresAt: rollbackNow + 60_000,
	})
	require.NoError(t, err)
	require.True(t, retry.Claimed, "a rolled-back claim must allow a fresh creation")

	// Expired reservations are evicted lazily for the requested key.
	expiredHash := make([]byte, 32)
	expiredHash[0] = 0xCC
	staleNow := time.Now().UnixMilli() - 120_000
	claim, err := store.ClaimGuildIdempotency(ctx, ClaimGuildIdempotencyParams{
		ActorUserID: actorUserID, Operation: "guild.channel.create", IdempotencyKey: "intent-3",
		RequestHash: expiredHash, ResourceID: 11111, CreatedAt: staleNow, ExpiresAt: staleNow + 1,
	})
	require.NoError(t, err)
	require.True(t, claim.Claimed)
	fresh, err := store.ClaimGuildIdempotency(ctx, ClaimGuildIdempotencyParams{
		ActorUserID: actorUserID, Operation: "guild.channel.create", IdempotencyKey: "intent-3",
		RequestHash: expiredHash, ResourceID: 22222, CreatedAt: time.Now().UnixMilli(), ExpiresAt: time.Now().UnixMilli() + 60_000,
	})
	require.NoError(t, err)
	require.True(t, fresh.Claimed)
	require.Equal(t, int64(22222), fresh.ResourceID)

	// Concurrent claims for one key create exactly one reservation.
	const concurrentKey = "intent-concurrent"
	concurrentHash := make([]byte, 32)
	concurrentHash[0] = 0xDD
	var wg sync.WaitGroup
	claims := make([]bool, 2)
	start := make(chan struct{})
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			now := time.Now().UnixMilli()
			value, err := store.ClaimGuildIdempotency(ctx, ClaimGuildIdempotencyParams{
				ActorUserID: actorUserID, Operation: "guild.invite.create", IdempotencyKey: concurrentKey,
				RequestHash: concurrentHash, ResourceID: 30000 + int64(i), CreatedAt: now, ExpiresAt: now + 60_000,
			})
			if err != nil {
				t.Errorf("concurrent claim failed: %v", err)
				return
			}
			claims[i] = value.Claimed
		}(i)
	}
	close(start)
	wg.Wait()
	require.Equal(t, 1, countTrue(claims), "exactly one concurrent claim may win")
	value, err := store.ClaimGuildIdempotency(ctx, ClaimGuildIdempotencyParams{
		ActorUserID: actorUserID, Operation: "guild.invite.create", IdempotencyKey: concurrentKey,
		RequestHash: concurrentHash, ResourceID: 40000, CreatedAt: time.Now().UnixMilli(), ExpiresAt: time.Now().UnixMilli() + 60_000,
	})
	require.NoError(t, err)
	require.False(t, value.Claimed)
	require.True(t, value.ResourceID == 30000 || value.ResourceID == 30001)
}
