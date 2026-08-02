package server

import (
	"sync"
	"testing"

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

func TestCreateUploadSignsExactImageContract(t *testing.T) {
	srv, assets, objects := newTestServer(t)
	req := newCreateRequest(
		mediav1.AssetKind_ASSET_KIND_USER_AVATAR,
		1024,
		"image/png",
	)

	resp, err := srv.CreateUpload(t.Context(), req)
	require.NoError(t, err)
	require.NotZero(t, resp.GetUploadId())
	require.NotEmpty(t, resp.GetPresignedUrl())
	require.Equal(t, mediav1.AssetStatus_ASSET_STATUS_CREATED, resp.GetStatus())
	require.False(t, resp.GetIdempotentReplay())
	require.Equal(t, map[string]string{
		"Content-Length": "1024",
		"Content-Type":   "image/png",
	}, resp.GetRequestHeaders())

	asset, err := assets.GetAsset(t.Context(), resp.GetUploadId())
	require.NoError(t, err)
	require.Equal(t, store.StatusCreated, asset.Status)
	require.Equal(t, int64(1001), asset.CreatedByUserID)
	require.Equal(t, int64(1001), asset.SubjectID)
	require.Equal(t, store.KindUserAvatar, asset.Kind)
	require.Equal(t, "r2", asset.StorageBackend)
	require.Equal(t, "staging/"+fmtID(resp.GetUploadId()), asset.StagingKey)
	require.Equal(t, asset.StagingKey, objects.lastPresignedKey)
	require.Equal(t, "image/png", objects.lastPresignedType)
	require.Equal(t, int64(1024), objects.lastPresignedLength)
	require.Equal(t, int64(900_000), asset.ExpiresAt-resp.GetExpiresAt())
}

func TestGetImageUploadConstraintsArePurposeSpecific(t *testing.T) {
	srv, _, _ := newTestServer(t)
	srv.svcCtx.Cfg.Media.ImageConstraints = config.ImageConstraintsConfig{
		UserAvatar: testImageConstraints(1<<20, 512, 512*512),
		GuildIcon:  testImageConstraints(2<<20, 1024, 1024*1024),
	}

	avatarReq := new(mediav1.GetImageUploadConstraintsRequest)
	avatarReq.SetUserAvatar(new(mediav1.UserAvatarUploadPurpose))
	avatarResp, err := srv.GetImageUploadConstraints(t.Context(), avatarReq)
	require.NoError(t, err)
	require.Equal(t, int64(1<<20), avatarResp.GetConstraints().GetMaxFileSizeBytes())
	require.Equal(t, int32(512), avatarResp.GetConstraints().GetMaxWidth())

	iconReq := new(mediav1.GetImageUploadConstraintsRequest)
	iconReq.SetGuildIcon(new(mediav1.GuildIconUploadPurpose))
	iconResp, err := srv.GetImageUploadConstraints(t.Context(), iconReq)
	require.NoError(t, err)
	require.Equal(t, int64(2<<20), iconResp.GetConstraints().GetMaxFileSizeBytes())
	require.Equal(t, int32(1024), iconResp.GetConstraints().GetMaxWidth())

	_, err = srv.GetImageUploadConstraints(t.Context(), new(mediav1.GetImageUploadConstraintsRequest))
	require.Error(t, err)
}

func TestCreateAttachmentUploadUsesTokenizedFinalKey(t *testing.T) {
	srv, assets, objects := newTestServer(t)
	req := newCreateRequest(
		mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT,
		1024,
		"application/pdf",
	)

	resp, err := srv.CreateUpload(t.Context(), req)
	require.NoError(t, err)
	asset, err := assets.GetAsset(t.Context(), resp.GetUploadId())
	require.NoError(t, err)
	require.Empty(t, asset.StagingKey)
	require.Equal(t, int64(3001), asset.SubjectID)
	require.Equal(t, "report.pdf", asset.Filename)
	require.Len(t, asset.StorageToken, 22)
	require.Equal(t, "attachments/3001/"+fmtID(asset.ID)+"/"+asset.StorageToken+"/report.pdf", asset.PublishedKey)
	require.Equal(t, asset.PublishedKey, objects.lastPresignedKey)
}

func TestCreateUploadUsesPurposeSpecificObjectStores(t *testing.T) {
	assetStore := newFakeStore()
	stagingStore := newFakeObjectStore()
	publicStore := newFakeObjectStore()
	attachmentStore := newFakeObjectStore()
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
			UserAvatar: testImageConstraints(10<<20, 4096, 4096*4096),
			GuildIcon:  testImageConstraints(10<<20, 4096, 4096*4096),
		},
		AttachmentImageInspection:    config.AttachmentImageInspectionProfile(testImageConstraints(10<<20, 4096, 4096*4096)),
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
		PublicObjectStore:     publicStore,
		StagingObjectStore:    stagingStore,
		AttachmentObjectStore: attachmentStore,
		Processor:             processing.NewProcessor(stagingStore, publicStore, mediaConfig),
	})

	source := testPNG(t, 2, 1)
	imageResp, err := srv.CreateUpload(t.Context(), newCreateRequest(
		mediav1.AssetKind_ASSET_KIND_USER_AVATAR,
		int64(len(source)),
		"image/png",
	))
	require.NoError(t, err)
	image, err := assetStore.GetAsset(t.Context(), imageResp.GetUploadId())
	require.NoError(t, err)
	require.Equal(t, image.StagingKey, stagingStore.lastPresignedKey)
	require.Empty(t, publicStore.lastPresignedKey)
	require.Empty(t, attachmentStore.lastPresignedKey)

	stagingStore.setObject(image.StagingKey, image.ContentType, source)
	_, err = srv.CompleteUpload(t.Context(), completeRequest(image.ID, 1001))
	require.NoError(t, err)
	require.False(t, stagingStore.hasObject(image.StagingKey))
	require.True(t, publicStore.hasObject(image.PublicKey()))

	attachmentResp, err := srv.CreateUpload(t.Context(), newCreateRequest(
		mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT,
		10,
		"application/octet-stream",
	))
	require.NoError(t, err)
	attachment, err := assetStore.GetAsset(t.Context(), attachmentResp.GetUploadId())
	require.NoError(t, err)
	require.Equal(t, attachment.PublishedKey, attachmentStore.lastPresignedKey)
}

func TestCreateGuildIconUploadUsesTypedSubject(t *testing.T) {
	srv, assets, _ := newTestServer(t)
	req := newCreateRequest(
		mediav1.AssetKind_ASSET_KIND_GUILD_ICON,
		1024,
		"image/png",
	)

	resp, err := srv.CreateUpload(t.Context(), req)
	require.NoError(t, err)
	asset, err := assets.GetAsset(t.Context(), resp.GetUploadId())
	require.NoError(t, err)
	require.Equal(t, int64(1001), asset.CreatedByUserID)
	require.Equal(t, int64(2001), asset.SubjectID)
	require.Equal(t, store.KindGuildIcon, asset.Kind)
	require.Equal(
		t,
		"icons/2001/"+fmtID(asset.ID),
		asset.PublicKey(),
	)
}

func TestCreateUploadValidation(t *testing.T) {
	srv, _, _ := newTestServer(t)
	tests := []struct {
		name string
		req  *mediav1.CreateUploadRequest
		code codes.Code
	}{
		{name: "actor user id required", req: new(mediav1.CreateUploadRequest), code: codes.InvalidArgument},
		{
			name: "purpose required",
			req: func() *mediav1.CreateUploadRequest {
				req := new(mediav1.CreateUploadRequest)
				req.SetActorUserId(1001)
				return req
			}(),
			code: codes.InvalidArgument,
		},
		{
			name: "guild id required",
			req: func() *mediav1.CreateUploadRequest {
				req := newCreateRequest(
					mediav1.AssetKind_ASSET_KIND_GUILD_ICON,
					1024,
					"image/png",
				)
				req.SetGuildIcon(new(mediav1.GuildIconUploadPurpose))
				return req
			}(),
			code: codes.InvalidArgument,
		},
		{
			name: "channel id required",
			req: func() *mediav1.CreateUploadRequest {
				req := newCreateRequest(
					mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT,
					1024,
					"application/octet-stream",
				)
				req.SetMessageAttachment(new(mediav1.MessageAttachmentUploadPurpose))
				return req
			}(),
			code: codes.InvalidArgument,
		},
		{
			name: "attachment filename invalid",
			req: func() *mediav1.CreateUploadRequest {
				req := newCreateRequest(
					mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT,
					1024,
					"application/octet-stream",
				)
				req.GetMessageAttachment().SetFilename("../secret")
				return req
			}(),
			code: codes.InvalidArgument,
		},
		{
			name: "size required",
			req: newCreateRequest(
				mediav1.AssetKind_ASSET_KIND_USER_AVATAR,
				0,
				"image/png",
			),
			code: codes.InvalidArgument,
		},
		{
			name: "image type rejected",
			req: newCreateRequest(
				mediav1.AssetKind_ASSET_KIND_USER_AVATAR,
				1024,
				"image/svg+xml",
			),
			code: codes.InvalidArgument,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := srv.CreateUpload(t.Context(), test.req)
			require.Equal(t, test.code, status.Code(err))
		})
	}
}

func TestCreateUploadQuotaIsAtomic(t *testing.T) {
	srv, _, _ := newTestServer(t)
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for range 12 {
		wg.Go(func() {
			_, err := srv.CreateUpload(t.Context(), newCreateRequest(
				mediav1.AssetKind_ASSET_KIND_USER_AVATAR,
				1024,
				"image/png",
			))
			results <- err
		})
	}
	wg.Wait()
	close(results)

	var successes, exhausted int
	for err := range results {
		switch status.Code(err) {
		case codes.OK:
			successes++
		case codes.ResourceExhausted:
			exhausted++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 5, successes)
	require.Equal(t, 7, exhausted)
}
