//go:build integration

package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"
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

	assetStore := NewPostgres(db)
	t.Run("create get and update", func(t *testing.T) {
		testCreateGetAndUpdate(t, assetStore)
	})
	t.Run("concurrent quota", func(t *testing.T) {
		testConcurrentQuota(t, assetStore)
	})
	t.Run("expired uploads", func(t *testing.T) {
		testExpiredUploads(t, assetStore)
	})
	t.Run("asset advisory lock", func(t *testing.T) {
		testAssetAdvisoryLock(t, assetStore)
	})
	t.Run("constraints", func(t *testing.T) {
		testConstraints(t, assetStore)
	})
}

func testCreateGetAndUpdate(t *testing.T, assetStore Store) {
	asset := integrationAsset(1001, 1101)
	require.NoError(t, assetStore.CreateAssetWithQuota(t.Context(), asset, 5))

	loaded, err := assetStore.GetAsset(t.Context(), asset.ID)
	require.NoError(t, err)
	require.Equal(t, asset.UserID, loaded.UserID)
	require.Equal(t, asset.StagingKey, loaded.StagingKey)

	lockedStore, unlock, err := assetStore.AcquireAssetLock(t.Context(), asset.ID)
	require.NoError(t, err)
	loaded, err = lockedStore.GetAsset(t.Context(), asset.ID)
	require.NoError(t, err)
	loaded.Status = StatusReady
	loaded.ActualSize = loaded.ExpectedSize
	loaded.Width = 64
	loaded.Height = 32
	loaded.SetVariants([]Variant{{
		Key:          "public/1001/64.webp",
		MaxDimension: 64,
		Width:        64,
		Height:       32,
		Size:         512,
	}})
	require.NoError(t, lockedStore.UpdateAsset(t.Context(), loaded))
	unlock()

	loaded, err = assetStore.GetAsset(t.Context(), asset.ID)
	require.NoError(t, err)
	require.Equal(t, StatusReady, loaded.Status)
	require.Equal(t, int64(1024), loaded.ActualSize)
	require.Len(t, loaded.Variants(), 1)
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
			results <- assetStore.CreateAssetWithQuota(t.Context(), asset, 5)
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
	invalid := integrationAsset(-1, 1401)
	err := assetStore.CreateAssetWithQuota(t.Context(), invalid, 5)
	var pqErr *pq.Error
	require.True(t, errors.As(err, &pqErr), "expected pq.Error, got %v", err)
	require.Equal(t, pq.ErrorCode("23514"), pqErr.Code)
}

func integrationAsset(id, userID int64) *Asset {
	now := time.Now().UnixMilli()
	return &Asset{
		ID:             id,
		UserID:         userID,
		Kind:           KindUserAvatar,
		Status:         StatusCreated,
		StorageBackend: "test",
		StagingKey:     fmt.Sprintf("staging/%d", id),
		ExpectedSize:   1024,
		ContentType:    "image/png",
		ExpiresAt:      now + int64(time.Minute/time.Millisecond),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func assetIDs(assets []*Asset) []int64 {
	ids := make([]int64, 0, len(assets))
	for _, asset := range assets {
		ids = append(ids, asset.ID)
	}
	return ids
}
