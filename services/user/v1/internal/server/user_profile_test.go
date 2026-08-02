package server

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/user/v1/internal/model"
)

func TestGetUserProfile(t *testing.T) {
	store := newFakeStore()
	store.profile = &model.UserProfile{
		UserID:        1001,
		Name:          "display name",
		AvatarAssetID: 77,
	}
	server := newTestUserServer(t, store)

	req := new(userv1.GetUserProfileRequest)
	req.SetUserId(1001)

	resp, err := server.GetUserProfile(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), resp.GetProfile().GetUserId())
	require.Equal(t, "display name", resp.GetProfile().GetName())
	require.Equal(t, int64(77), resp.GetProfile().GetAvatarAssetId())
}

func TestBatchGetUserProfiles(t *testing.T) {
	store := newFakeStore()
	store.batchProfiles = []*model.UserProfile{
		{UserID: 1001, Username: "alice", Name: "Alice"},
		{UserID: 1002, Username: "bob", Name: "Bob"},
	}
	server := newTestUserServer(t, store)

	req := new(userv1.BatchGetUserProfilesRequest)
	req.SetUserIds([]int64{1002, 1001, 1002})
	resp, err := server.BatchGetUserProfiles(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []int64{1002, 1001}, store.listProfileIDs)
	require.Len(t, resp.GetProfiles(), 2)
	require.Equal(t, int64(1001), resp.GetProfiles()[0].GetUserId())
	require.Equal(t, int64(1002), resp.GetProfiles()[1].GetUserId())
}

func TestBatchGetUserProfilesValidation(t *testing.T) {
	server := newTestUserServer(t, newFakeStore())

	req := new(userv1.BatchGetUserProfilesRequest)
	req.SetUserIds([]int64{0})
	_, err := server.BatchGetUserProfiles(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	req.SetUserIds(make([]int64, maxUserProfileBatch+1))
	_, err = server.BatchGetUserProfiles(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestUpdateUserProfile(t *testing.T) {
	store := newFakeStore()
	store.profile = &model.UserProfile{
		UserID:        1001,
		Username:      "test_user",
		Name:          "old name",
		Bio:           "old bio",
		AvatarAssetID: 77,
		CreatedAt:     10,
		UpdatedAt:     20,
	}
	publisher := new(fakeUserPublisher)
	server := newTestUserServerWithPublisher(t, store, &fakeMediaClient{}, publisher)

	req := new(userv1.UpdateUserProfileRequest)
	req.SetUserId(1001)
	req.SetName(" new name ")

	resp, err := server.UpdateUserProfile(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "new name", resp.GetProfile().GetName())
	require.Equal(t, "old bio", resp.GetProfile().GetBio())
	require.Equal(t, int64(77), resp.GetProfile().GetAvatarAssetId())
	assertProfileUpdatedEvent(t, publisher, store.profile)
}

func TestUpdateUserProfileBioAndAvatar(t *testing.T) {
	store := newFakeStore()
	store.profile = &model.UserProfile{
		UserID:        1001,
		Username:      "test_user",
		Name:          "name",
		Bio:           "keep me",
		AvatarAssetID: 77,
	}
	mediaClient := &fakeMediaClient{asset: avatarAsset(7001, 1001)}
	publisher := new(fakeUserPublisher)
	server := newTestUserServerWithPublisher(t, store, mediaClient, publisher)

	req := new(userv1.UpdateUserProfileRequest)
	req.SetUserId(1001)
	req.SetBio("hello 简介")
	req.SetAvatarAssetId(7001)

	resp, err := server.UpdateUserProfile(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "name", resp.GetProfile().GetName())
	require.Equal(t, "hello 简介", resp.GetProfile().GetBio())
	require.Equal(t, int64(7001), resp.GetProfile().GetAvatarAssetId())
	require.Equal(t, int64(1001), mediaClient.completeRequest.GetActorUserId())
	require.Equal(t, int64(7001), mediaClient.completeRequest.GetUploadId())
	assertProfileUpdatedEvent(t, publisher, store.profile)

	clearReq := new(userv1.UpdateUserProfileRequest)
	clearReq.SetUserId(1001)
	clearReq.SetBio("")
	clearReq.SetAvatarAssetId(0)
	clearResp, err := server.UpdateUserProfile(context.Background(), clearReq)
	require.NoError(t, err)
	require.Empty(t, clearResp.GetProfile().GetBio())
	require.Zero(t, clearResp.GetProfile().GetAvatarAssetId())
}

func TestUpdateUserProfileMountsReadyAvatarWithoutComplete(t *testing.T) {
	store := newFakeStore()
	store.profile = &model.UserProfile{UserID: 1001, Name: "name", AvatarAssetID: 11}
	asset := avatarAsset(7001, 1001)
	asset.SetStatus(mediav1.AssetStatus_ASSET_STATUS_READY)
	mediaClient := &fakeMediaClient{asset: asset}
	server := newTestUserServerWithMedia(t, store, mediaClient)

	req := new(userv1.UpdateUserProfileRequest)
	req.SetUserId(1001)
	req.SetAvatarAssetId(7001)
	resp, err := server.UpdateUserProfile(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(7001), resp.GetProfile().GetAvatarAssetId())
	require.Nil(t, mediaClient.completeRequest)
}

func TestUpdateUserProfileRejectsForeignAvatar(t *testing.T) {
	store := newFakeStore()
	store.profile = &model.UserProfile{UserID: 1001, AvatarAssetID: 99}
	mediaClient := &fakeMediaClient{asset: avatarAsset(7001, 2002)}
	server := newTestUserServerWithMedia(t, store, mediaClient)

	req := new(userv1.UpdateUserProfileRequest)
	req.SetUserId(1001)
	req.SetAvatarAssetId(7001)
	_, err := server.UpdateUserProfile(context.Background(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.Nil(t, mediaClient.completeRequest)
	require.Equal(t, int64(99), store.profile.AvatarAssetID)
}

func TestUpdateUserProfileValidation(t *testing.T) {
	server := newTestUserServer(t, newFakeStore())

	req := new(userv1.UpdateUserProfileRequest)
	req.SetUserId(1001)

	_, err := server.UpdateUserProfile(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	bioReq := new(userv1.UpdateUserProfileRequest)
	bioReq.SetUserId(1001)
	bioReq.SetBio(strings.Repeat("刺", maxBioRunes+1))
	_, err = server.UpdateUserProfile(context.Background(), bioReq)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGetUserProfileByUsername(t *testing.T) {
	store := newFakeStore()
	store.profile = &model.UserProfile{
		UserID:   1001,
		Username: "alice",
		Name:     "Alice",
	}
	server := newTestUserServer(t, store)

	t.Run("found case insensitive", func(t *testing.T) {
		req := new(userv1.GetUserProfileByUsernameRequest)
		req.SetUsername("aLiCe")
		resp, err := server.GetUserProfileByUsername(context.Background(), req)
		require.NoError(t, err)
		require.Equal(t, "alice", resp.GetProfile().GetUsername())
	})

	t.Run("not found", func(t *testing.T) {
		req := new(userv1.GetUserProfileByUsernameRequest)
		req.SetUsername("unknown")
		_, err := server.GetUserProfileByUsername(context.Background(), req)
		require.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("invalid format", func(t *testing.T) {
		req := new(userv1.GetUserProfileByUsernameRequest)
		req.SetUsername("a")
		_, err := server.GetUserProfileByUsername(context.Background(), req)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestUpdateUsername(t *testing.T) {
	store := newFakeStore()
	store.profile = &model.UserProfile{UserID: 1001, Username: "old_name", Name: "Display"}
	publisher := new(fakeUserPublisher)
	server := newTestUserServerWithPublisher(t, store, &fakeMediaClient{}, publisher)

	req := new(userv1.UpdateUsernameRequest)
	req.SetUserId(1001)
	req.SetUsername("  New_Name42  ")
	resp, err := server.UpdateUsername(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "new_name42", resp.GetProfile().GetUsername())
	require.Equal(t, "new_name42", store.profile.Username)
	assertProfileUpdatedEvent(t, publisher, store.profile)
}

func TestUpdateUsernameValidationAndConflicts(t *testing.T) {
	store := newFakeStore()
	store.profile = &model.UserProfile{UserID: 1001, Username: "old_name"}
	server := newTestUserServer(t, store)

	req := new(userv1.UpdateUsernameRequest)
	req.SetUsername("valid_name")
	_, err := server.UpdateUsername(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	req.SetUserId(1001)
	req.SetUsername("bad name!")
	_, err = server.UpdateUsername(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	req.SetUsername("valid_name")
	store.updateUsernameErr = &pgconn.PgError{Code: "23505", ConstraintName: "user_profiles_username_active_idx"}
	_, err = server.UpdateUsername(context.Background(), req)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.UserDomain, rpcerror.UserUsernameTaken))

	store.updateUsernameErr = nil
	req.SetUserId(9999)
	_, err = server.UpdateUsername(context.Background(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
}
