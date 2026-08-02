package server

import (
	"bytes"
	"testing"
	"time"

	sn "github.com/bwmarrin/snowflake"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	"github.com/soasurs/cordis/services/media/v1/config"
	"github.com/soasurs/cordis/services/media/v1/internal/processing"
	"github.com/soasurs/cordis/services/media/v1/internal/store"
	"github.com/soasurs/cordis/services/media/v1/internal/svc"
)

func TestGetAssetReturnsCreatorAndSubject(t *testing.T) {
	srv, assets, _ := newTestServer(t)
	asset := &store.Asset{
		ID:              123,
		CreatedByUserID: 1001,
		SubjectID:       2001,
		Kind:            store.KindGuildIcon,
		Status:          store.StatusReady,
	}
	assets.createAsset(asset)

	req := new(mediav1.GetAssetRequest)
	req.SetAssetId(asset.ID)
	resp, err := srv.GetAsset(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, asset.CreatedByUserID, resp.GetAsset().GetCreatedByUserId())
	require.Equal(t, asset.SubjectID, resp.GetAsset().GetSubjectId())
	require.Equal(
		t,
		mediav1.AssetKind_ASSET_KIND_GUILD_ICON,
		resp.GetAsset().GetKind(),
	)
}

func TestCompleteImageUploadPublishesBeforeDeletingStaging(t *testing.T) {
	srv, assets, objects := newTestServer(t)
	source := testPNG(t, 96, 48)
	createResp, err := srv.CreateUpload(t.Context(), newCreateRequest(
		mediav1.AssetKind_ASSET_KIND_USER_AVATAR,
		int64(len(source)),
		"image/png",
	))
	require.NoError(t, err)
	asset, err := assets.GetAsset(t.Context(), createResp.GetUploadId())
	require.NoError(t, err)
	objects.setObject(asset.StagingKey, "image/png", source)

	resp, err := srv.CompleteUpload(t.Context(), completeRequest(asset.ID, 1001))
	require.NoError(t, err)
	require.Equal(t, asset.ID, resp.GetAssetId())
	require.Equal(t, int64(len(source)), resp.GetMetadata().GetSize())
	require.Equal(t, "image/png", resp.GetMetadata().GetContentType())
	require.Equal(t, int32(96), resp.GetMetadata().GetWidth())
	require.Equal(t, int32(48), resp.GetMetadata().GetHeight())
	require.NotEmpty(t, resp.GetMetadata().GetBlurhash())
	require.False(t, objects.hasObject(asset.StagingKey))
	require.True(t, objects.hasObject("avatars/1001/"+fmtID(asset.ID)))

	retry, err := srv.CompleteUpload(t.Context(), completeRequest(asset.ID, 1001))
	require.NoError(t, err)
	require.Equal(t, resp.GetAssetId(), retry.GetAssetId())
	require.Equal(t, resp.GetMetadata().GetContentType(), retry.GetMetadata().GetContentType())
}

func TestCompleteUploadResumesCompleting(t *testing.T) {
	for _, initialStatus := range []store.Status{store.StatusCompleting} {
		t.Run(string(initialStatus), func(t *testing.T) {
			srv, assets, objects := newTestServer(t)
			source := testPNG(t, 20, 10)
			asset := &store.Asset{
				ID:              123,
				CreatedByUserID: 1001,
				SubjectID:       1001,
				Kind:            store.KindUserAvatar,
				Status:          initialStatus,
				StagingKey:      "staging/123",
				ExpectedSize:    int64(len(source)),
				ActualSize:      int64(len(source)),
				ContentType:     "image/png",
			}
			assets.createAsset(asset)
			objects.setObject(asset.StagingKey, "image/png", source)

			resp, err := srv.CompleteUpload(t.Context(), completeRequest(asset.ID, 1001))
			require.NoError(t, err)
			require.Equal(t, asset.ID, resp.GetAssetId())
			require.Equal(t, store.StatusReady, asset.Status)
		})
	}
}

func TestCompleteOpaqueUploadKeepsPrivateObject(t *testing.T) {
	srv, assets, objects := newTestServer(t)
	source := []byte("opaque attachment")
	createResp, err := srv.CreateUpload(t.Context(), newCreateRequest(
		mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT,
		int64(len(source)),
		"application/octet-stream",
	))
	require.NoError(t, err)
	asset, err := assets.GetAsset(t.Context(), createResp.GetUploadId())
	require.NoError(t, err)
	objects.setObject(asset.PublishedKey, asset.ContentType, source)

	resp, err := srv.CompleteUpload(t.Context(), completeRequest(asset.ID, 1001))
	require.NoError(t, err)
	require.Equal(t, "application/octet-stream", resp.GetMetadata().GetContentType())
	require.Equal(t, "report.pdf", resp.GetMetadata().GetFilename())
	require.Contains(t, resp.GetMetadata().GetUrl(), "/report.pdf")
	require.Zero(t, resp.GetMetadata().GetUrlExpiresAt())
	require.Empty(t, resp.GetMetadata().GetBlurhash())
	require.True(t, objects.hasObject(asset.PublishedKey))
	require.Equal(t, store.StatusReady, asset.Status)
}

func TestCompleteAttachmentImageUploadSetsBlurhash(t *testing.T) {
	srv, assets, objects := newTestServer(t)
	source := testPNG(t, 64, 32)
	createResp, err := srv.CreateUpload(t.Context(), newCreateRequest(
		mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT,
		int64(len(source)),
		"image/png",
	))
	require.NoError(t, err)
	asset, err := assets.GetAsset(t.Context(), createResp.GetUploadId())
	require.NoError(t, err)
	objects.setObject(asset.PublishedKey, asset.ContentType, source)

	resp, err := srv.CompleteUpload(t.Context(), completeRequest(asset.ID, 1001))
	require.NoError(t, err)
	require.Equal(t, int32(64), resp.GetMetadata().GetWidth())
	require.Equal(t, int32(32), resp.GetMetadata().GetHeight())
	require.NotEmpty(t, resp.GetMetadata().GetBlurhash())
	require.Equal(t, "image/png", resp.GetMetadata().GetContentType())
	require.True(t, objects.hasObject(asset.PublishedKey))

	loaded, err := assets.GetAsset(t.Context(), asset.ID)
	require.NoError(t, err)
	require.Equal(t, resp.GetMetadata().GetBlurhash(), loaded.Blurhash)
	require.Equal(t, int32(64), loaded.Width)
	require.Equal(t, int32(32), loaded.Height)
}

func TestCompleteAttachmentImageUploadSkipsOversizedObjectRead(t *testing.T) {
	assetStore := newFakeStore()
	objStore := newFakeObjectStore()
	node, err := sn.NewNode(1)
	require.NoError(t, err)
	mediaConfig := config.MediaConfig{
		UploadSessionTTLSeconds:      1800,
		PresignedURLTTLSeconds:       900,
		MaxUploadSizeBytes:           524288000,
		MaxActiveUploadsPerUser:      5,
		ImageProcessingTimeoutMs:     30000,
		MaxConcurrentImageProcessing: 2,
		ImageConstraints: config.ImageConstraintsConfig{
			UserAvatar: testImageConstraints(64, 4096, 4096*4096),
			GuildIcon:  testImageConstraints(64, 4096, 4096*4096),
		},
		AttachmentImageInspection:    config.AttachmentImageInspectionProfile(testImageConstraints(64, 4096, 4096*4096)),
		AttachmentAccessMode:         config.AttachmentAccessPublic,
		AttachmentDownloadTTLSeconds: 3600,
	}
	srv := New(&svc.ServiceContext{
		Cfg: config.Config{
			ObjectStore: config.ObjectStoreConfig{
				Backend:                 "r2",
				AttachmentPublicBaseURL: "https://cdn.example.com",
			},
			Media: mediaConfig,
		},
		Store:                 assetStore,
		Snowflake:             node,
		PublicObjectStore:     objStore,
		StagingObjectStore:    objStore,
		AttachmentObjectStore: objStore,
		Processor:             processing.NewProcessor(objStore, objStore, mediaConfig),
	})
	source := testPNG(t, 64, 32)
	require.Greater(t, len(source), 64)

	createResp, err := srv.CreateUpload(t.Context(), newCreateRequest(
		mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT,
		int64(len(source)),
		"image/png",
	))
	require.NoError(t, err)
	asset, err := assetStore.GetAsset(t.Context(), createResp.GetUploadId())
	require.NoError(t, err)
	objStore.setObject(asset.PublishedKey, asset.ContentType, source)

	resp, err := srv.CompleteUpload(t.Context(), completeRequest(asset.ID, 1001))
	require.NoError(t, err)
	require.Zero(t, resp.GetMetadata().GetWidth())
	require.Zero(t, resp.GetMetadata().GetHeight())
	require.Empty(t, resp.GetMetadata().GetBlurhash())
	require.Equal(t, "image/png", resp.GetMetadata().GetContentType())
	require.True(t, objStore.hasObject(asset.PublishedKey))
}

func TestCompleteUploadRejectsObjectMetadataMismatch(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		data        []byte
	}{
		{name: "size", contentType: "image/png", data: []byte("short")},
		{name: "content type", contentType: "image/jpeg", data: bytes.Repeat([]byte("x"), 10)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv, assets, objects := newTestServer(t)
			asset := &store.Asset{
				ID:              123,
				CreatedByUserID: 1001,
				SubjectID:       1001,
				Kind:            store.KindUserAvatar,
				Status:          store.StatusCreated,
				StagingKey:      "staging/123",
				ExpectedSize:    10,
				ContentType:     "image/png",
			}
			assets.createAsset(asset)
			objects.setObject(asset.StagingKey, test.contentType, test.data)

			_, err := srv.CompleteUpload(t.Context(), completeRequest(asset.ID, 1001))
			require.Equal(t, codes.FailedPrecondition, status.Code(err))
			require.Equal(t, store.StatusFailed, asset.Status)
			require.False(t, objects.hasObject(asset.StagingKey))
		})
	}
}

func TestCompleteAndAbortVerifyOwner(t *testing.T) {
	srv, assets, _ := newTestServer(t)
	asset := &store.Asset{ID: 123, CreatedByUserID: 1001, Status: store.StatusCreated}
	assets.createAsset(asset)

	_, err := srv.CompleteUpload(t.Context(), completeRequest(asset.ID, 2002))
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	abortReq := new(mediav1.AbortUploadRequest)
	abortReq.SetUploadId(asset.ID)
	abortReq.SetActorUserId(2002)
	_, err = srv.AbortUpload(t.Context(), abortReq)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestAbortUploadIsIdempotentAndPreservesReadyAsset(t *testing.T) {
	srv, assets, _ := newTestServer(t)
	asset := &store.Asset{
		ID:              123,
		CreatedByUserID: 1001,
		SubjectID:       1001,
		Kind:            store.KindUserAvatar,
		Status:          store.StatusCreated,
		StagingKey:      "staging/123",
	}
	assets.createAsset(asset)
	req := new(mediav1.AbortUploadRequest)
	req.SetUploadId(asset.ID)
	req.SetActorUserId(1001)

	_, err := srv.AbortUpload(t.Context(), req)
	require.NoError(t, err)
	_, err = srv.AbortUpload(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, store.StatusAborted, asset.Status)

	ready := &store.Asset{ID: 124, CreatedByUserID: 1001, Status: store.StatusReady}
	assets.createAsset(ready)
	req.SetUploadId(ready.ID)
	_, err = srv.AbortUpload(t.Context(), req)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	require.Equal(t, store.StatusReady, ready.Status)
}

func TestBatchGetAssetURLsUsesConfiguredAccessMode(t *testing.T) {
	srv, assets, objects := newTestServer(t)
	attachment := &store.Asset{
		ID:             123,
		Kind:           store.KindMessageAttachment,
		Status:         store.StatusReady,
		PublishedKey:   "attachments/10/123/token/report.pdf",
		StorageBackend: "r2",
	}
	assets.createAsset(attachment)
	req := new(mediav1.BatchGetAssetURLsRequest)
	req.SetAssetIds([]int64{attachment.ID, attachment.ID})

	resp, err := srv.BatchGetAssetURLs(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetAssets(), 1)
	require.Equal(t, "https://cdn.example.com/attachments/10/123/token/report.pdf", resp.GetAssets()[0].GetUrl())
	require.Zero(t, resp.GetAssets()[0].GetExpiresAt())
	require.Empty(t, objects.lastDownloadKey)

	image := &store.Asset{
		ID:           124,
		Kind:         store.KindUserAvatar,
		Status:       store.StatusReady,
		PublishedKey: "avatars/10/124",
	}
	assets.createAsset(image)
	req.SetAssetIds([]int64{image.ID})
	_, err = srv.BatchGetAssetURLs(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	srv.svcCtx.Cfg.Media.AttachmentAccessMode = config.AttachmentAccessPresigned
	req.SetAssetIds([]int64{attachment.ID})
	resp, err = srv.BatchGetAssetURLs(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, "https://s3.example.com/"+attachment.PublishedKey, resp.GetAssets()[0].GetUrl())
	require.Equal(t, attachment.PublishedKey, objects.lastDownloadKey)
	require.Greater(t, resp.GetAssets()[0].GetExpiresAt(), time.Now().UnixMilli())
}
