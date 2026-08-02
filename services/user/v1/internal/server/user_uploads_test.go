package server

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/services/user/v1/internal/model"
)

func TestGetAvatarUploadConstraints(t *testing.T) {
	fakeStore := newFakeStore()
	mediaClient := &fakeMediaClient{}
	server := newTestUserServerWithMedia(t, fakeStore, mediaClient)

	resp, err := server.GetAvatarUploadConstraints(t.Context(), new(userv1.GetAvatarUploadConstraintsRequest))
	require.NoError(t, err)
	require.Equal(t, int64(10485760), resp.GetConstraints().GetMaxFileSizeBytes())
	require.Equal(t, int32(4096), resp.GetConstraints().GetMaxWidth())
	require.Equal(t, int32(4096), resp.GetConstraints().GetMaxHeight())
	require.Equal(t, int64(16777216), resp.GetConstraints().GetMaxPixels())
	require.Equal(t, []string{"image/jpeg", "image/png", "image/webp"}, resp.GetConstraints().GetAllowedContentTypes())
}

func TestAvatarUploadLifecycle(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.profile = &model.UserProfile{UserID: 1001, Name: "user"}
	mediaClient := &fakeMediaClient{asset: avatarAsset(7001, 1001)}
	publisher := new(fakeUserPublisher)
	server := newTestUserServerWithPublisher(t, fakeStore, mediaClient, publisher)

	createReq := new(userv1.CreateAvatarUploadRequest)
	createReq.SetUserId(1001)
	createReq.SetExpectedSize(123)
	createReq.SetContentType("image/png")
	createReq.SetIdempotencyKey("avatar-intent-1")
	createResp, err := server.CreateAvatarUpload(t.Context(), createReq)
	require.NoError(t, err)
	require.Equal(t, int64(7001), createResp.GetUploadId())
	require.Equal(t, map[string]string{"Content-Type": "image/png"}, createResp.GetRequestHeaders())
	require.Equal(t, mediav1.AssetStatus_ASSET_STATUS_CREATED, createResp.GetStatus())
	require.False(t, createResp.GetIdempotentReplay())
	require.Equal(t, int64(1001), mediaClient.createRequest.GetActorUserId())
	require.True(t, mediaClient.createRequest.HasUserAvatar())
	require.Equal(t, "avatar-intent-1", mediaClient.createRequest.GetIdempotencyKey())

	completeReq := new(userv1.CompleteAvatarUploadRequest)
	completeReq.SetUserId(1001)
	completeReq.SetUploadId(7001)
	completeResp, err := server.CompleteAvatarUpload(t.Context(), completeReq)
	require.NoError(t, err)
	require.Equal(t, int64(7001), completeResp.GetProfile().GetAvatarAssetId())
	require.Equal(t, int64(1001), mediaClient.completeRequest.GetActorUserId())
	require.Equal(t, int64(7001), mediaClient.completeRequest.GetUploadId())
	assertProfileUpdatedEvent(t, publisher, fakeStore.profile)
}

func TestCompleteAvatarUploadRejectsAnotherUsersAsset(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.profile = &model.UserProfile{UserID: 1001, AvatarAssetID: 99}
	mediaClient := &fakeMediaClient{asset: avatarAsset(7001, 2002)}
	server := newTestUserServerWithMedia(t, fakeStore, mediaClient)

	req := new(userv1.CompleteAvatarUploadRequest)
	req.SetUserId(1001)
	req.SetUploadId(7001)
	_, err := server.CompleteAvatarUpload(t.Context(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.Nil(t, mediaClient.completeRequest)
	require.Equal(t, int64(99), fakeStore.profile.AvatarAssetID)
}
