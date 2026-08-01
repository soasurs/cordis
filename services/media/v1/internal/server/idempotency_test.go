package server

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/media/v1/config"
	"github.com/soasurs/cordis/services/media/v1/internal/store"
)

func TestIdempotencyExpiryNeverBelowUploadSessionTTL(t *testing.T) {
	cfg := config.Config{
		Idempotency: config.IdempotencyConfig{CreateUploadTTLSeconds: 60},
		Media:       config.MediaConfig{UploadSessionTTLSeconds: 1800},
	}
	require.Equal(t, int64(1000+1800_000), idempotencyExpiry(1000, cfg))

	cfg.Idempotency.CreateUploadTTLSeconds = 7200
	require.Equal(t, int64(1000+7200_000), idempotencyExpiry(1000, cfg))
}

func TestCreateUploadFingerprintsAreStableAndDistinct(t *testing.T) {
	first, err := createUploadRequestHash(1001, store.KindUserAvatar, 1001, 1024, "image/png", "")
	require.NoError(t, err)
	second, err := createUploadRequestHash(1001, store.KindUserAvatar, 1001, 1024, "image/png", "")
	require.NoError(t, err)
	require.True(t, bytes.Equal(first, second))

	different, err := createUploadRequestHash(1001, store.KindUserAvatar, 1001, 2048, "image/png", "")
	require.NoError(t, err)
	require.False(t, bytes.Equal(first, different))

	attachment, err := createUploadRequestHash(1001, store.KindMessageAttachment, 3001, 4096, "application/pdf", "report.pdf")
	require.NoError(t, err)
	attachmentRetry, err := createUploadRequestHash(1001, store.KindMessageAttachment, 3001, 4096, "application/pdf", "report.pdf")
	require.NoError(t, err)
	require.True(t, bytes.Equal(attachment, attachmentRetry))
	attachmentRenamed, err := createUploadRequestHash(1001, store.KindMessageAttachment, 3001, 4096, "application/pdf", "other.pdf")
	require.NoError(t, err)
	require.False(t, bytes.Equal(attachment, attachmentRenamed))
}

func TestValidateIdempotencyKey(t *testing.T) {
	require.NoError(t, validateIdempotencyKey("intent-1", 255))
	require.Equal(t, codes.InvalidArgument, status.Code(validateIdempotencyKey("", 255)))
	require.Equal(t, codes.InvalidArgument, status.Code(validateIdempotencyKey(" intent", 255)))
	require.Equal(t, codes.InvalidArgument, status.Code(validateIdempotencyKey("intent ", 255)))
	require.Equal(t, codes.InvalidArgument, status.Code(validateIdempotencyKey("long", 3)))
}

func TestCreateUploadIdempotentReplayReturnsSameUpload(t *testing.T) {
	srv, assets, _ := newTestServer(t)
	req := newCreateRequest(mediav1.AssetKind_ASSET_KIND_USER_AVATAR, 1024, "image/png")
	req.SetIdempotencyKey("avatar-intent-1")

	first, err := srv.CreateUpload(t.Context(), req)
	require.NoError(t, err)
	require.NotZero(t, first.GetUploadId())
	require.NotEmpty(t, first.GetPresignedUrl())
	require.Equal(t, mediav1.AssetStatus_ASSET_STATUS_CREATED, first.GetStatus())
	require.False(t, first.GetIdempotentReplay())

	second, err := srv.CreateUpload(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, first.GetUploadId(), second.GetUploadId())
	require.Equal(t, first.GetPresignedUrl(), second.GetPresignedUrl())
	require.Equal(t, mediav1.AssetStatus_ASSET_STATUS_CREATED, second.GetStatus())
	require.True(t, second.GetIdempotentReplay())

	asset, err := assets.GetAsset(t.Context(), first.GetUploadId())
	require.NoError(t, err)
	require.Equal(t, store.StatusCreated, asset.Status)
	require.Len(t, assets.assets, 1)
}

func TestCreateUploadIdempotentReplayIssuesNewPresignedURL(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := newCreateRequest(mediav1.AssetKind_ASSET_KIND_USER_AVATAR, 1024, "image/png")
	req.SetIdempotencyKey("avatar-intent-url")

	first, err := srv.CreateUpload(t.Context(), req)
	require.NoError(t, err)
	require.NotEmpty(t, first.GetPresignedUrl())

	req.SetExpectedSize(2048)
	_, err = srv.CreateUpload(t.Context(), req)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.MediaDomain, rpcerror.MediaIdempotencyKeyReused))
}

func TestCreateUploadIdempotentReplayReturnsStoredStatusWithoutURL(t *testing.T) {
	for _, initialStatus := range []store.Status{
		store.StatusCompleting,
		store.StatusReady,
		store.StatusFailed,
		store.StatusAborted,
		store.StatusExpired,
	} {
		t.Run(string(initialStatus), func(t *testing.T) {
			srv, assets, _ := newTestServer(t)
			req := newCreateRequest(mediav1.AssetKind_ASSET_KIND_GUILD_ICON, 2048, "image/webp")
			req.SetIdempotencyKey("icon-intent-" + string(initialStatus))

			first, err := srv.CreateUpload(t.Context(), req)
			require.NoError(t, err)

			asset, err := assets.GetAsset(t.Context(), first.GetUploadId())
			require.NoError(t, err)
			asset.Status = initialStatus

			second, err := srv.CreateUpload(t.Context(), req)
			require.NoError(t, err)
			require.Equal(t, first.GetUploadId(), second.GetUploadId())
			require.Equal(t, assetStatusToProto(initialStatus), second.GetStatus())
			require.True(t, second.GetIdempotentReplay())
			require.Empty(t, second.GetPresignedUrl())
			require.Zero(t, second.GetExpiresAt())
		})
	}
}

func TestCreateUploadIdempotencyKeyScopedToActor(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := newCreateRequest(mediav1.AssetKind_ASSET_KIND_USER_AVATAR, 1024, "image/png")
	req.SetIdempotencyKey("shared-key")

	first, err := srv.CreateUpload(t.Context(), req)
	require.NoError(t, err)

	other := newCreateRequest(mediav1.AssetKind_ASSET_KIND_USER_AVATAR, 1024, "image/png")
	other.SetActorUserId(2002)
	other.SetIdempotencyKey("shared-key")
	second, err := srv.CreateUpload(t.Context(), other)
	require.NoError(t, err)
	require.NotEqual(t, first.GetUploadId(), second.GetUploadId())
}

func TestCreateUploadIdempotencyKeyValidation(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := newCreateRequest(mediav1.AssetKind_ASSET_KIND_USER_AVATAR, 1024, "image/png")

	req.SetIdempotencyKey("")
	_, err := srv.CreateUpload(t.Context(), req)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	req.SetIdempotencyKey("  padded  ")
	_, err = srv.CreateUpload(t.Context(), req)
	require.Error(t, err)

	longKey := make([]byte, 256)
	req.SetIdempotencyKey(string(longKey))
	_, err = srv.CreateUpload(t.Context(), req)
	require.Error(t, err)
}

func TestCreateUploadIdempotentReplayUsesStoredFingerprint(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := newCreateRequest(mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT, 4096, "application/pdf")
	req.SetIdempotencyKey("attachment-intent-1")

	_, err := srv.CreateUpload(t.Context(), req)
	require.NoError(t, err)

	changed := newCreateRequest(mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT, 4096, "application/pdf")
	changed.SetIdempotencyKey("attachment-intent-1")
	changed.GetMessageAttachment().SetFilename("other.pdf")
	_, err = srv.CreateUpload(t.Context(), changed)
	require.Error(t, err)
	require.True(t, rpcerror.Is(err, rpcerror.MediaDomain, rpcerror.MediaIdempotencyKeyReused))
}
