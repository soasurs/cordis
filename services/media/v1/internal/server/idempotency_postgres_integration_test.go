//go:build integration

package server

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/migration"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/media/v1/config"
	mediamigrations "github.com/soasurs/cordis/services/media/v1/db/migrations"
	"github.com/soasurs/cordis/services/media/v1/internal/processing"
	"github.com/soasurs/cordis/services/media/v1/internal/store"
	"github.com/soasurs/cordis/services/media/v1/internal/svc"
)

func newPostgresMediaService(t *testing.T) (store.Store, *MediaServer) {
	return newPostgresMediaServiceWithLimit(t, 5)
}

func newPostgresMediaServiceWithLimit(t *testing.T, activeUploadLimit int64) (store.Store, *MediaServer) {
	t.Helper()
	postgres := testkit.StartPostgres(t)
	migrationDB, err := database.NewPostgres(database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, migrationDB.Close()) })
	db, err := database.NewPostgresPool(t.Context(), database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(db.Close)
	require.NoError(t, migration.Apply(t.Context(), migrationDB, mediamigrations.FS))

	node, err := snowflake.New()
	require.NoError(t, err)
	mediaStore := store.New(db)
	objStore := newFakeObjectStore()
	mediaConfig := config.MediaConfig{
		UploadSessionTTLSeconds:      1800,
		PresignedURLTTLSeconds:       900,
		MaxUploadSizeBytes:           524288000,
		MaxActiveUploadsPerUser:      activeUploadLimit,
		ImageProcessingTimeoutMs:     30000,
		MaxConcurrentImageProcessing: 2,
		ImageConstraints: config.ImageConstraintsConfig{
			UserAvatar: testImageConstraints(10<<20, 4096, 4096*4096),
			GuildIcon:  testImageConstraints(10<<20, 4096, 4096*4096),
		},
		AttachmentImageInspection:    config.AttachmentImageInspectionProfile(testImageConstraints(10<<20, 4096, 4096*4096)),
		AttachmentAccessMode:         config.AttachmentAccessPublic,
		AttachmentDownloadTTLSeconds: 3600,
	}
	svcCtx := &svc.ServiceContext{
		Cfg: config.Config{
			Media: mediaConfig,
		},
		Store:                 mediaStore,
		Snowflake:             node,
		PublicObjectStore:     objStore,
		StagingObjectStore:    objStore,
		AttachmentObjectStore: objStore,
		Processor:             processing.NewProcessor(objStore, objStore, mediaConfig),
	}
	return mediaStore, New(svcCtx)
}

func TestCreateUploadIdempotentReplayWithPostgres(t *testing.T) {
	mediaStore, service := newPostgresMediaService(t)
	ctx := t.Context()

	req := newCreateRequest(mediav1.AssetKind_ASSET_KIND_USER_AVATAR, 1024, "image/png")
	req.SetIdempotencyKey("avatar-intent-1")
	first, err := service.CreateUpload(ctx, req)
	require.NoError(t, err)
	uploadID := first.GetUploadId()
	require.NotEmpty(t, first.GetPresignedUrl())

	replay, err := service.CreateUpload(ctx, req)
	require.NoError(t, err)
	require.Equal(t, uploadID, replay.GetUploadId())
	require.NotEmpty(t, replay.GetPresignedUrl(), "CREATED asset must get a fresh presigned URL")

	asset, err := mediaStore.GetAsset(ctx, uploadID)
	require.NoError(t, err)
	require.Equal(t, store.StatusCreated, asset.Status)

	assets, err := mediaStore.ListAssets(ctx, []int64{uploadID, uploadID})
	require.NoError(t, err)
	require.Len(t, assets, 1, "replay must not create another asset")
}

func TestCreateUploadRejectsIdempotencyKeyReuseWithPostgres(t *testing.T) {
	mediaStore, service := newPostgresMediaService(t)
	ctx := t.Context()

	first := newCreateRequest(mediav1.AssetKind_ASSET_KIND_USER_AVATAR, 1024, "image/png")
	first.SetIdempotencyKey("avatar-intent-1")
	resp, err := service.CreateUpload(ctx, first)
	require.NoError(t, err)
	uploadID := resp.GetUploadId()

	second := newCreateRequest(mediav1.AssetKind_ASSET_KIND_USER_AVATAR, 2048, "image/png")
	second.SetIdempotencyKey("avatar-intent-1")
	_, err = service.CreateUpload(ctx, second)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.MediaDomain, rpcerror.MediaIdempotencyKeyReused))

	assets, err := mediaStore.ListAssets(ctx, []int64{uploadID, uploadID})
	require.NoError(t, err)
	require.Len(t, assets, 1, "reused key must not create another asset")
}

func TestCreateUploadConcurrentIdempotentRequestsWithPostgres(t *testing.T) {
	mediaStore, service := newPostgresMediaService(t)
	ctx := t.Context()

	req := newCreateRequest(mediav1.AssetKind_ASSET_KIND_GUILD_ICON, 2048, "image/webp")
	req.SetIdempotencyKey("icon-intent-concurrent")

	var wg sync.WaitGroup
	uploadIDs := make([]int64, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			resp, err := service.CreateUpload(ctx, req)
			errs[i] = err
			if resp != nil {
				uploadIDs[i] = resp.GetUploadId()
			}
		}(i)
	}
	close(start)
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, uploadIDs[0], uploadIDs[1], "concurrent requests must share one upload")

	assets, err := mediaStore.ListAssets(ctx, []int64{uploadIDs[0], uploadIDs[0]})
	require.NoError(t, err)
	require.Len(t, assets, 1)
}

func TestCreateUploadReplaySkipsQuota(t *testing.T) {
	_, service := newPostgresMediaServiceWithLimit(t, 1)
	ctx := t.Context()

	req := newCreateRequest(mediav1.AssetKind_ASSET_KIND_USER_AVATAR, 1024, "image/png")
	req.SetIdempotencyKey("avatar-intent-quota")
	resp, err := service.CreateUpload(ctx, req)
	require.NoError(t, err)

	replay, err := service.CreateUpload(ctx, req)
	require.NoError(t, err)
	require.Equal(t, resp.GetUploadId(), replay.GetUploadId())

	other := newCreateRequest(mediav1.AssetKind_ASSET_KIND_USER_AVATAR, 2048, "image/png")
	other.SetIdempotencyKey("avatar-intent-quota-other")
	_, err = service.CreateUpload(ctx, other)
	require.Equal(t, codes.ResourceExhausted, status.Code(err), "replay must not consume the only quota slot")
}

func TestCreateUploadReplayPreservesFinishedStatus(t *testing.T) {
	mediaStore, service := newPostgresMediaService(t)
	ctx := t.Context()

	req := newCreateRequest(mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT, 4096, "application/pdf")
	req.SetIdempotencyKey("attachment-intent-1")
	first, err := service.CreateUpload(ctx, req)
	require.NoError(t, err)
	uploadID := first.GetUploadId()

	asset, err := mediaStore.GetAsset(ctx, uploadID)
	require.NoError(t, err)
	asset.Status = store.StatusReady
	require.NoError(t, mediaStore.UpdateAsset(ctx, asset))

	replay, err := service.CreateUpload(ctx, req)
	require.NoError(t, err)
	require.Equal(t, uploadID, replay.GetUploadId())
	require.Empty(t, replay.GetPresignedUrl(), "finished asset must not get a PUT URL")

	asset, err = mediaStore.GetAsset(ctx, uploadID)
	require.NoError(t, err)
	require.Equal(t, store.StatusReady, asset.Status, "replay must not change finished status")
}

func TestCreateUploadFailedCreationRollsBackClaim(t *testing.T) {
	mediaStore, service := newPostgresMediaServiceWithLimit(t, 1)
	ctx := t.Context()

	req := newCreateRequest(mediav1.AssetKind_ASSET_KIND_USER_AVATAR, 1024, "image/png")
	req.SetIdempotencyKey("avatar-intent-1")
	first, err := service.CreateUpload(ctx, req)
	require.NoError(t, err)

	failed := newCreateRequest(mediav1.AssetKind_ASSET_KIND_USER_AVATAR, 2048, "image/png")
	failed.SetIdempotencyKey("avatar-intent-fail")
	_, err = service.CreateUpload(ctx, failed)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))

	asset, err := mediaStore.GetAsset(ctx, first.GetUploadId())
	require.NoError(t, err)
	asset.Status = store.StatusAborted
	require.NoError(t, mediaStore.UpdateAsset(ctx, asset))

	retry, err := service.CreateUpload(ctx, failed)
	require.NoError(t, err)
	require.NotZero(t, retry.GetUploadId(), "rolled-back claim must allow a fresh creation attempt")
}
