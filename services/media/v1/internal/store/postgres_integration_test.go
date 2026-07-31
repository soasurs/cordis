//go:build integration

package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/migration"
	mediamigrations "github.com/soasurs/cordis/services/media/v1/db/migrations"
)

func TestMediaStoreWithPostgres(t *testing.T) {
	postgres := testkit.StartPostgres(t)
	db, err := database.NewPostgres(database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, migration.Apply(t.Context(), db, mediamigrations.FS))

	assetStore := New(db)
	t.Run("create get and update", func(t *testing.T) {
		testCreateGetAndUpdate(t, assetStore)
	})
	t.Run("concurrent quota", func(t *testing.T) {
		testConcurrentQuota(t, assetStore)
	})
	t.Run("list assets", func(t *testing.T) {
		testListAssets(t, assetStore)
	})
	t.Run("expired uploads", func(t *testing.T) {
		testExpiredUploads(t, assetStore)
	})
	t.Run("asset advisory lock", func(t *testing.T) {
		testAssetAdvisoryLock(t, assetStore)
	})
	t.Run("media idempotency claims", func(t *testing.T) {
		testMediaIdempotency(t, assetStore)
	})
	t.Run("constraints", func(t *testing.T) {
		testConstraints(t, assetStore)
	})
}

func testMediaIdempotency(t *testing.T, store Store) {
	ctx := t.Context()
	hash := []byte("12345678901234567890123456789012")

	claim, err := store.ClaimMediaIdempotency(ctx, ClaimMediaIdempotencyParams{
		ActorUserID:    9701,
		Operation:      "media.create.user_avatar",
		IdempotencyKey: "intent-1",
		RequestHash:    hash,
		AssetID:        5701,
		CreatedAt:      1000,
		ExpiresAt:      2000,
	})
	require.NoError(t, err)
	require.True(t, claim.Claimed)
	require.Equal(t, int64(5701), claim.AssetID)

	claim, err = store.ClaimMediaIdempotency(ctx, ClaimMediaIdempotencyParams{
		ActorUserID:    9701,
		Operation:      "media.create.user_avatar",
		IdempotencyKey: "intent-1",
		RequestHash:    []byte("abcdefghijklmnopqrstuvwxyz123456"),
		AssetID:        5702,
		CreatedAt:      1100,
		ExpiresAt:      2100,
	})
	require.NoError(t, err)
	require.False(t, claim.Claimed)
	require.Equal(t, int64(5701), claim.AssetID)
	require.Equal(t, hash, claim.RequestHash)

	err = store.Transact(ctx, func(tx Store) error {
		claim, err := tx.ClaimMediaIdempotency(ctx, ClaimMediaIdempotencyParams{
			ActorUserID:    9701,
			Operation:      "media.create.user_avatar",
			IdempotencyKey: "intent-rollback",
			RequestHash:    hash,
			AssetID:        5703,
			CreatedAt:      1000,
			ExpiresAt:      2000,
		})
		require.NoError(t, err)
		require.True(t, claim.Claimed)
		return errors.New("force idempotency rollback")
	})
	require.Error(t, err)

	claim, err = store.ClaimMediaIdempotency(ctx, ClaimMediaIdempotencyParams{
		ActorUserID:    9701,
		Operation:      "media.create.user_avatar",
		IdempotencyKey: "intent-rollback",
		RequestHash:    hash,
		AssetID:        5704,
		CreatedAt:      1000,
		ExpiresAt:      2000,
	})
	require.NoError(t, err)
	require.True(t, claim.Claimed)
	require.Equal(t, int64(5704), claim.AssetID)

	claim, err = store.ClaimMediaIdempotency(ctx, ClaimMediaIdempotencyParams{
		ActorUserID:    9701,
		Operation:      "media.create.user_avatar",
		IdempotencyKey: "intent-expired",
		RequestHash:    hash,
		AssetID:        5705,
		CreatedAt:      1000,
		ExpiresAt:      2000,
	})
	require.NoError(t, err)
	require.True(t, claim.Claimed)
	claim, err = store.ClaimMediaIdempotency(ctx, ClaimMediaIdempotencyParams{
		ActorUserID:    9701,
		Operation:      "media.create.user_avatar",
		IdempotencyKey: "intent-expired",
		RequestHash:    hash,
		AssetID:        5706,
		CreatedAt:      2000,
		ExpiresAt:      3000,
	})
	require.NoError(t, err)
	require.True(t, claim.Claimed)
	require.Equal(t, int64(5706), claim.AssetID)

	claim, err = store.ClaimMediaIdempotency(ctx, ClaimMediaIdempotencyParams{
		ActorUserID:    9701,
		Operation:      "media.create.guild_icon",
		IdempotencyKey: "intent-1",
		RequestHash:    hash,
		AssetID:        5707,
		CreatedAt:      1000,
		ExpiresAt:      2000,
	})
	require.NoError(t, err)
	require.True(t, claim.Claimed)

	const concurrentRequests = 16
	results := make(chan *MediaIdempotencyClaim, concurrentRequests)
	errs := make(chan error, concurrentRequests)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range concurrentRequests {
		wg.Go(func() {
			<-start
			err := store.Transact(ctx, func(tx Store) error {
				claim, err := tx.ClaimMediaIdempotency(ctx, ClaimMediaIdempotencyParams{
					ActorUserID:    9701,
					Operation:      "media.create.user_avatar",
					IdempotencyKey: "intent-concurrent",
					RequestHash:    hash,
					AssetID:        int64(5800 + i),
					CreatedAt:      1000,
					ExpiresAt:      2000,
				})
				if err == nil {
					results <- claim
				}
				return err
			})
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)

	claims := make([]*MediaIdempotencyClaim, 0, concurrentRequests)
	for claim := range results {
		claims = append(claims, claim)
	}
	claimedCount := 0
	var winnerID int64
	for _, claim := range claims {
		if claim.Claimed {
			claimedCount++
			winnerID = claim.AssetID
		}
	}
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 1, claimedCount)
	require.NotZero(t, winnerID)
}

func testListAssets(t *testing.T, assetStore Store) {
	first := integrationAsset(2501, 1251)
	second := integrationAsset(2502, 1251)
	require.NoError(t, assetStore.CreateAssetWithQuota(t.Context(), first, 5))
	require.NoError(t, assetStore.CreateAssetWithQuota(t.Context(), second, 5))

	assets, err := assetStore.ListAssets(t.Context(), []int64{second.ID, first.ID, 9999})
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{first.ID, second.ID}, assetIDs(assets))

	assets, err = assetStore.ListAssets(t.Context(), nil)
	require.NoError(t, err)
	require.Empty(t, assets)
}

func testCreateGetAndUpdate(t *testing.T, assetStore Store) {
	asset := integrationAsset(1001, 1101)
	require.NoError(t, assetStore.CreateAssetWithQuota(t.Context(), asset, 5))

	loaded, err := assetStore.GetAsset(t.Context(), asset.ID)
	require.NoError(t, err)
	require.Equal(t, asset.CreatedByUserID, loaded.CreatedByUserID)
	require.Equal(t, asset.SubjectID, loaded.SubjectID)
	require.Equal(t, asset.StagingKey, loaded.StagingKey)

	lockedStore, unlock, err := assetStore.AcquireAssetLock(t.Context(), asset.ID)
	require.NoError(t, err)
	loaded, err = lockedStore.GetAsset(t.Context(), asset.ID)
	require.NoError(t, err)
	loaded.Status = StatusReady
	loaded.ActualSize = loaded.ExpectedSize
	loaded.Width = 64
	loaded.Height = 32
	loaded.Blurhash = "LEHV6nWB2yk8pyo0adR*.7kCMdnj"
	loaded.PublishedKey = "avatars/1101/1001"
	require.NoError(t, lockedStore.UpdateAsset(t.Context(), loaded))
	unlock()

	loaded, err = assetStore.GetAsset(t.Context(), asset.ID)
	require.NoError(t, err)
	require.Equal(t, StatusReady, loaded.Status)
	require.Equal(t, int64(1024), loaded.ActualSize)
	require.Equal(t, "avatars/1101/1001", loaded.PublishedKey)
	require.Equal(t, "LEHV6nWB2yk8pyo0adR*.7kCMdnj", loaded.Blurhash)
}

func testConcurrentQuota(t *testing.T, assetStore Store) {
	const userID = int64(1201)
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for index := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			asset := integrationAsset(2000+int64(index), userID)
			err := assetStore.Transact(t.Context(), func(tx Store) error {
				return tx.CreateAssetWithQuota(t.Context(), asset, 5)
			})
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	var created, rejected int
	for err := range results {
		switch {
		case err == nil:
			created++
		case errors.Is(err, ErrActiveUploadLimit):
			rejected++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 5, created)
	require.Equal(t, 7, rejected)
}

func testExpiredUploads(t *testing.T, assetStore Store) {
	expired := integrationAsset(3001, 1301)
	expired.ExpiresAt = time.Now().Add(-time.Minute).UnixMilli()
	require.NoError(t, assetStore.CreateAssetWithQuota(t.Context(), expired, 5))

	active := integrationAsset(3002, 1301)
	active.ExpiresAt = time.Now().Add(time.Minute).UnixMilli()
	require.NoError(t, assetStore.CreateAssetWithQuota(t.Context(), active, 5))

	assets, err := assetStore.ListExpiredUploads(t.Context(), time.Now().UnixMilli())
	require.NoError(t, err)
	require.Contains(t, assetIDs(assets), expired.ID)
	require.NotContains(t, assetIDs(assets), active.ID)
}

func testAssetAdvisoryLock(t *testing.T, assetStore Store) {
	_, firstUnlock, err := assetStore.AcquireAssetLock(t.Context(), 4001)
	require.NoError(t, err)

	acquired := make(chan func(), 1)
	lockCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() {
		_, unlock, lockErr := assetStore.AcquireAssetLock(lockCtx, 4001)
		if lockErr == nil {
			acquired <- unlock
		}
	}()

	select {
	case unlock := <-acquired:
		unlock()
		t.Fatal("second asset lock acquired before the first was released")
	case <-time.After(100 * time.Millisecond):
	}
	firstUnlock()

	select {
	case unlock := <-acquired:
		unlock()
	case <-time.After(5 * time.Second):
		t.Fatal("second asset lock did not acquire after release")
	}
}

func testConstraints(t *testing.T, assetStore Store) {
	t.Run("asset id", func(t *testing.T) {
		invalid := integrationAsset(-1, 1401)
		err := assetStore.CreateAssetWithQuota(t.Context(), invalid, 5)
		var pgErr *pgconn.PgError
		require.True(t, errors.As(err, &pgErr), "expected pgconn.PgError, got %v", err)
		require.Equal(t, "23514", pgErr.Code)
	})

	t.Run("subject id", func(t *testing.T) {
		invalid := integrationAsset(1402, 1401)
		invalid.SubjectID = 0
		err := assetStore.CreateAssetWithQuota(t.Context(), invalid, 5)
		var pgErr *pgconn.PgError
		require.True(t, errors.As(err, &pgErr), "expected pgconn.PgError, got %v", err)
		require.Equal(t, "23514", pgErr.Code)
	})
}

func integrationAsset(id, userID int64) *Asset {
	now := time.Now().UnixMilli()
	return &Asset{
		ID:              id,
		CreatedByUserID: userID,
		SubjectID:       userID,
		Kind:            KindUserAvatar,
		Status:          StatusCreated,
		StorageBackend:  "test",
		StagingKey:      fmt.Sprintf("staging/%d", id),
		ExpectedSize:    1024,
		ContentType:     "image/png",
		ExpiresAt:       now + int64(time.Minute/time.Millisecond),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func assetIDs(assets []*Asset) []int64 {
	ids := make([]int64, 0, len(assets))
	for _, asset := range assets {
		ids = append(ids, asset.ID)
	}
	return ids
}
