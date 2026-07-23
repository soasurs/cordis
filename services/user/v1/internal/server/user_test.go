package server

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"testing"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	"github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/user/v1/internal/model"
	"github.com/soasurs/cordis/services/user/v1/internal/store"
	"github.com/soasurs/cordis/services/user/v1/internal/svc"
)

func TestCreateUser(t *testing.T) {
	store := newFakeStore()
	server := newTestUserServer(t, store)

	req := new(userv1.CreateUserRequest)
	req.SetName("display name")
	req.SetEmail("user@example.com")
	req.SetUsername("tester")

	resp, err := server.CreateUser(context.Background(), req)
	require.NoError(t, err)
	require.NotZero(t, resp.GetUser().GetUserId())
	require.Equal(t, "user@example.com", resp.GetUser().GetEmail())
	require.NotNil(t, store.profile)
	require.Equal(t, "display name", store.profile.Name)
	require.Equal(t, "tester", store.profile.Username)
}

func TestCreateUserValidation(t *testing.T) {
	server := newTestUserServer(t, newFakeStore())

	req := new(userv1.CreateUserRequest)
	req.SetEmail("user@example.com")
	req.SetUsername("tester")

	_, err := server.CreateUser(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestCreateUserEmailAlreadyExists(t *testing.T) {
	store := newFakeStore()
	store.createUserErr = &pq.Error{Code: "23505"}
	server := newTestUserServer(t, store)

	req := new(userv1.CreateUserRequest)
	req.SetName("display name")
	req.SetEmail("user@example.com")
	req.SetUsername("tester")

	_, err := server.CreateUser(context.Background(), req)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.UserDomain, rpcerror.UserEmailAlreadyExists))
}

func TestGetUser(t *testing.T) {
	store := newFakeStore()
	store.user = &model.User{
		UserID: 1001,
		Email:  "user@example.com",
	}
	server := newTestUserServer(t, store)

	req := new(userv1.GetUserRequest)
	req.SetUserId(1001)

	resp, err := server.GetUser(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), resp.GetUser().GetUserId())
	require.Equal(t, "user@example.com", resp.GetUser().GetEmail())
}

func TestGetUserWithEmail(t *testing.T) {
	store := newFakeStore()
	store.user = &model.User{
		UserID: 1001,
		Email:  "user@example.com",
	}
	server := newTestUserServer(t, store)

	req := new(userv1.GetUserRequest)
	req.SetEmail("user@example.com")

	resp, err := server.GetUser(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), resp.GetUser().GetUserId())
	require.Equal(t, "user@example.com", resp.GetUser().GetEmail())
}

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
		Name:          "old name",
		AvatarAssetID: 77,
	}
	server := newTestUserServer(t, store)

	req := new(userv1.UpdateUserProfileRequest)
	req.SetUserId(1001)
	req.SetName(" new name ")

	resp, err := server.UpdateUserProfile(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "new name", resp.GetProfile().GetName())
	require.Equal(t, int64(77), resp.GetProfile().GetAvatarAssetId())
}

func TestUpdateUserProfileValidation(t *testing.T) {
	server := newTestUserServer(t, newFakeStore())

	req := new(userv1.UpdateUserProfileRequest)
	req.SetUserId(1001)

	_, err := server.UpdateUserProfile(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestCheckEmailAvailability(t *testing.T) {
	store := newFakeStore()
	store.emailAvailable = true
	server := newTestUserServer(t, store)

	req := new(userv1.CheckEmailAvailabilityRequest)
	req.SetEmail("user@example.com")

	resp, err := server.CheckEmailAvailability(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.GetAvailable())
}

func TestUpdateEmail(t *testing.T) {
	store := newFakeStore()
	store.user = &model.User{
		UserID: 1001,
		Email:  "old@example.com",
	}
	server := newTestUserServer(t, store)

	req := new(userv1.UpdateEmailRequest)
	req.SetUserId(1001)
	req.SetEmail("new@example.com")

	resp, err := server.UpdateEmail(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "new@example.com", resp.GetUser().GetEmail())
}

func TestUpdateEmailValidation(t *testing.T) {
	store := newFakeStore()
	store.user = &model.User{UserID: 1001, Email: "old@example.com"}
	server := newTestUserServer(t, store)

	t.Run("missing user id", func(t *testing.T) {
		req := new(userv1.UpdateEmailRequest)
		req.SetEmail("new@example.com")
		_, err := server.UpdateEmail(context.Background(), req)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty email", func(t *testing.T) {
		req := new(userv1.UpdateEmailRequest)
		req.SetUserId(1001)
		_, err := server.UpdateEmail(context.Background(), req)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("invalid email format", func(t *testing.T) {
		req := new(userv1.UpdateEmailRequest)
		req.SetUserId(1001)
		req.SetEmail("not-an-email")
		_, err := server.UpdateEmail(context.Background(), req)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestCreateUserNameTooLong(t *testing.T) {
	server := newTestUserServer(t, newFakeStore())

	req := new(userv1.CreateUserRequest)
	req.SetName(string(make([]byte, 65)))
	req.SetEmail("user@example.com")
	req.SetUsername("tester")

	_, err := server.CreateUser(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestCreateUserInvalidEmail(t *testing.T) {
	server := newTestUserServer(t, newFakeStore())

	req := new(userv1.CreateUserRequest)
	req.SetName("name")
	req.SetEmail("no-at-sign")
	req.SetUsername("tester")

	_, err := server.CreateUser(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGetUserNotFound(t *testing.T) {
	server := newTestUserServer(t, newFakeStore())

	req := new(userv1.GetUserRequest)
	req.SetUserId(9999)

	_, err := server.GetUser(context.Background(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestAvatarUploadLifecycle(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.profile = &model.UserProfile{UserID: 1001, Name: "user"}
	mediaClient := &fakeMediaClient{asset: avatarAsset(7001, 1001)}
	server := newTestUserServerWithMedia(t, fakeStore, mediaClient)

	createReq := new(userv1.CreateAvatarUploadRequest)
	createReq.SetUserId(1001)
	createReq.SetExpectedSize(123)
	createReq.SetContentType("image/png")
	createResp, err := server.CreateAvatarUpload(t.Context(), createReq)
	require.NoError(t, err)
	require.Equal(t, int64(7001), createResp.GetUploadId())
	require.Equal(t, int64(1001), mediaClient.createRequest.GetActorUserId())
	require.True(t, mediaClient.createRequest.HasUserAvatar())

	completeReq := new(userv1.CompleteAvatarUploadRequest)
	completeReq.SetUserId(1001)
	completeReq.SetUploadId(7001)
	completeResp, err := server.CompleteAvatarUpload(t.Context(), completeReq)
	require.NoError(t, err)
	require.Equal(t, int64(7001), completeResp.GetProfile().GetAvatarAssetId())
	require.Equal(t, int64(1001), mediaClient.completeRequest.GetActorUserId())
	require.Equal(t, int64(7001), mediaClient.completeRequest.GetUploadId())
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

func newTestUserServer(t *testing.T, store store.Store) userv1.UserServiceServer {
	return newTestUserServerWithMedia(t, store, &fakeMediaClient{})
}

func newTestUserServerWithMedia(
	t *testing.T,
	store store.Store,
	mediaClient mediav1.MediaServiceClient,
) userv1.UserServiceServer {
	t.Helper()

	node, err := snowflake.New()
	require.NoError(t, err)

	return New(&svc.ServiceContext{
		Store:       store,
		Snowflake:   node,
		MediaClient: mediaClient,
	})
}

type fakeMediaClient struct {
	mediav1.MediaServiceClient
	asset           *mediav1.Asset
	createRequest   *mediav1.CreateUploadRequest
	completeRequest *mediav1.CompleteUploadRequest
	abortRequest    *mediav1.AbortUploadRequest
}

func (f *fakeMediaClient) CreateUpload(
	_ context.Context,
	req *mediav1.CreateUploadRequest,
	_ ...grpc.CallOption,
) (*mediav1.CreateUploadResponse, error) {
	f.createRequest = req
	resp := new(mediav1.CreateUploadResponse)
	resp.SetUploadId(7001)
	resp.SetPresignedUrl("https://upload.example/7001")
	resp.SetExpiresAt(9001)
	return resp, nil
}

func (f *fakeMediaClient) GetAsset(
	_ context.Context,
	_ *mediav1.GetAssetRequest,
	_ ...grpc.CallOption,
) (*mediav1.GetAssetResponse, error) {
	resp := new(mediav1.GetAssetResponse)
	resp.SetAsset(f.asset)
	return resp, nil
}

func (f *fakeMediaClient) CompleteUpload(
	_ context.Context,
	req *mediav1.CompleteUploadRequest,
	_ ...grpc.CallOption,
) (*mediav1.CompleteUploadResponse, error) {
	f.completeRequest = req
	resp := new(mediav1.CompleteUploadResponse)
	resp.SetAssetId(req.GetUploadId())
	return resp, nil
}

func (f *fakeMediaClient) AbortUpload(
	_ context.Context,
	req *mediav1.AbortUploadRequest,
	_ ...grpc.CallOption,
) (*mediav1.AbortUploadResponse, error) {
	f.abortRequest = req
	return new(mediav1.AbortUploadResponse), nil
}

func avatarAsset(assetID, userID int64) *mediav1.Asset {
	asset := new(mediav1.Asset)
	asset.SetId(assetID)
	asset.SetCreatedByUserId(userID)
	asset.SetSubjectId(userID)
	asset.SetKind(mediav1.AssetKind_ASSET_KIND_USER_AVATAR)
	asset.SetStatus(mediav1.AssetStatus_ASSET_STATUS_CREATED)
	return asset
}

type fakeStore struct {
	user                *model.User
	profile             *model.UserProfile
	batchProfiles       []*model.UserProfile
	listProfileIDs      []int64
	createUserErr       error
	createProfileErr    error
	updateUsernameErr   error
	getUserWithEmailErr error
	emailAvailable      bool
	relationships       map[[2]int64]*model.Relationship
	lockedPairs         [][2]int64
}

func (s *fakeStore) LockRelationshipPair(_ context.Context, userID, targetID int64) error {
	s.lockedPairs = append(s.lockedPairs, [2]int64{userID, targetID})
	return nil
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		relationships: make(map[[2]int64]*model.Relationship),
	}
}

func (s *fakeStore) Transact(ctx context.Context, fn func(txStore store.Store) error) error {
	return fn(s)
}

func (s *fakeStore) CreateUser(_ context.Context, userID int64, email string) (*model.User, error) {
	if s.createUserErr != nil {
		return nil, s.createUserErr
	}
	s.user = &model.User{
		UserID: userID,
		Email:  email,
	}
	return s.user, nil
}

func (s *fakeStore) GetUser(_ context.Context, userID int64) (*model.User, error) {
	if s.user == nil || s.user.UserID != userID {
		return nil, sql.ErrNoRows
	}
	return s.user, nil
}

func (s *fakeStore) GetUserWithEmail(_ context.Context, email string) (*model.User, error) {
	if s.getUserWithEmailErr != nil {
		return nil, s.getUserWithEmailErr
	}
	if s.user == nil || s.user.Email != email {
		return nil, sql.ErrNoRows
	}
	return s.user, nil
}

func (s *fakeStore) CheckEmailAvailability(context.Context, string) (bool, error) {
	return s.emailAvailable, nil
}

func (s *fakeStore) UpdateUserEmail(_ context.Context, userID int64, email string) (*model.User, error) {
	if s.user == nil || s.user.UserID != userID {
		return nil, sql.ErrNoRows
	}
	if s.user.Email != email {
		s.user.Email = email
		s.user.EmailVerifiedAt = 0
	}
	return s.user, nil
}

func (s *fakeStore) MarkUserEmailVerified(_ context.Context, userID int64, email string, verifiedAt int64) error {
	if s.user == nil || s.user.UserID != userID || s.user.Email != email {
		return sql.ErrNoRows
	}
	s.user.EmailVerifiedAt = verifiedAt
	return nil
}

func (s *fakeStore) CreateUserProfile(_ context.Context, userID int64, username, name string) (*model.UserProfile, error) {
	if s.createProfileErr != nil {
		return nil, s.createProfileErr
	}
	if s.user == nil || s.user.UserID != userID {
		return nil, errors.New("missing user")
	}
	s.profile = &model.UserProfile{
		UserID:   userID,
		Username: username,
		Name:     name,
	}
	return s.profile, nil
}

func (s *fakeStore) GetUserProfile(_ context.Context, userID int64) (*model.UserProfile, error) {
	if s.profile == nil || s.profile.UserID != userID {
		return nil, sql.ErrNoRows
	}
	return s.profile, nil
}

func (s *fakeStore) ListUserProfiles(_ context.Context, userIDs []int64) ([]*model.UserProfile, error) {
	s.listProfileIDs = append([]int64(nil), userIDs...)
	return s.batchProfiles, nil
}

func (s *fakeStore) UpdateUsername(_ context.Context, userID int64, username string) (*model.UserProfile, error) {
	if s.profile == nil || s.profile.UserID != userID {
		return nil, sql.ErrNoRows
	}
	if s.updateUsernameErr != nil {
		return nil, s.updateUsernameErr
	}
	s.profile.Username = username
	return s.profile, nil
}

func (s *fakeStore) GetUserProfileByUsername(_ context.Context, username string) (*model.UserProfile, error) {
	if s.profile == nil || s.profile.Username == "" || s.profile.Username != username {
		return nil, sql.ErrNoRows
	}
	return s.profile, nil
}

func (s *fakeStore) UpdateUserProfile(_ context.Context, params store.UpdateUserProfileParams) (*model.UserProfile, error) {
	if s.profile == nil || s.profile.UserID != params.UserID {
		return nil, sql.ErrNoRows
	}
	if params.Name != nil {
		s.profile.Name = *params.Name
	}
	return s.profile, nil
}

func (s *fakeStore) UpdateUserAvatar(_ context.Context, userID, assetID int64) (*model.UserProfile, error) {
	if s.profile == nil || s.profile.UserID != userID {
		return nil, sql.ErrNoRows
	}
	s.profile.AvatarAssetID = assetID
	return s.profile, nil
}

func (s *fakeStore) UpsertRelationship(_ context.Context, rel *model.Relationship) error {
	key := [2]int64{rel.UserID, rel.TargetID}
	if existing, ok := s.relationships[key]; ok {
		existing.Type = rel.Type
		existing.UpdatedAt = rel.CreatedAt
	} else {
		s.relationships[key] = &model.Relationship{
			UserID:    rel.UserID,
			TargetID:  rel.TargetID,
			Type:      rel.Type,
			CreatedAt: rel.CreatedAt,
			UpdatedAt: 0,
		}
	}
	return nil
}

func (s *fakeStore) GetRelationship(_ context.Context, userID, targetID int64) (*model.Relationship, error) {
	key := [2]int64{userID, targetID}
	if rel, ok := s.relationships[key]; ok {
		return rel, nil
	}
	return nil, sql.ErrNoRows
}

func (s *fakeStore) DeleteRelationship(_ context.Context, userID, targetID int64) error {
	key := [2]int64{userID, targetID}
	if _, ok := s.relationships[key]; !ok {
		return sql.ErrNoRows
	}
	delete(s.relationships, key)
	return nil
}

func (s *fakeStore) DeleteRelationshipExceptBlocked(_ context.Context, userID, targetID int64) error {
	key := [2]int64{userID, targetID}
	if rel, ok := s.relationships[key]; ok && rel.Type != model.RelationshipBlocked {
		delete(s.relationships, key)
	}
	return nil
}

func (s *fakeStore) ListRelationships(_ context.Context, params store.ListRelationshipsParams) ([]*model.Relationship, error) {
	var result []*model.Relationship
	for key, rel := range s.relationships {
		if key[0] != params.UserID {
			continue
		}
		if params.Type != 0 && rel.Type != params.Type {
			continue
		}
		if params.BeforeTargetID != 0 && rel.TargetID >= params.BeforeTargetID {
			continue
		}
		result = append(result, rel)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].TargetID > result[j].TargetID
	})
	if len(result) > params.Limit {
		result = result[:params.Limit]
	}
	return result, nil
}

func (s *fakeStore) ListRelationshipsBidirectional(_ context.Context, userID int64, targetIDs []int64) ([]*model.Relationship, error) {
	var relationships []*model.Relationship
	for _, targetID := range targetIDs {
		if relationship, ok := s.relationships[[2]int64{userID, targetID}]; ok {
			value := *relationship
			relationships = append(relationships, &value)
		}
		if relationship, ok := s.relationships[[2]int64{targetID, userID}]; ok {
			value := *relationship
			relationships = append(relationships, &value)
		}
	}
	return relationships, nil
}

func (s *fakeStore) ListRelationshipsByTargets(_ context.Context, userID int64, targetIDs []int64) ([]*model.Relationship, error) {
	targetSet := make(map[int64]bool, len(targetIDs))
	for _, id := range targetIDs {
		targetSet[id] = true
	}
	var result []*model.Relationship
	for key, rel := range s.relationships {
		if key[0] == userID && targetSet[key[1]] {
			result = append(result, rel)
		}
	}
	return result, nil
}

func TestMarkEmailVerified(t *testing.T) {
	store := newFakeStore()
	store.user = &model.User{UserID: 1001, Email: "user@example.com"}
	server := newTestUserServer(t, store)

	req := new(userv1.MarkEmailVerifiedRequest)
	req.SetUserId(1001)
	req.SetEmail("user@example.com")
	req.SetVerifiedAt(4001)

	resp, err := server.MarkEmailVerified(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Equal(t, int64(4001), store.user.EmailVerifiedAt)
}

func TestMarkEmailVerifiedStaleEmail(t *testing.T) {
	store := newFakeStore()
	store.user = &model.User{UserID: 1001, Email: "new@example.com"}
	server := newTestUserServer(t, store)

	req := new(userv1.MarkEmailVerifiedRequest)
	req.SetUserId(1001)
	req.SetEmail("old@example.com")
	_, err := server.MarkEmailVerified(context.Background(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
	require.Zero(t, store.user.EmailVerifiedAt)
}

func TestUpdateEmailClearsVerification(t *testing.T) {
	store := newFakeStore()
	store.user = &model.User{
		UserID:          1001,
		Email:           "old@example.com",
		EmailVerifiedAt: 4001,
	}
	server := newTestUserServer(t, store)

	req := new(userv1.UpdateEmailRequest)
	req.SetUserId(1001)
	req.SetEmail("new@example.com")

	resp, err := server.UpdateEmail(context.Background(), req)
	require.NoError(t, err)
	require.Zero(t, resp.GetUser().GetEmailVerifiedAt())
	require.Zero(t, store.user.EmailVerifiedAt)
}

func TestEmailsAreNormalizedToLowercase(t *testing.T) {
	store := newFakeStore()
	server := newTestUserServer(t, store)

	req := new(userv1.CreateUserRequest)
	req.SetName("display name")
	req.SetEmail("  Alice@Example.COM ")
	req.SetUsername("tester")
	resp, err := server.CreateUser(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "alice@example.com", resp.GetUser().GetEmail())

	getReq := new(userv1.GetUserRequest)
	getReq.SetEmail("ALICE@example.com")
	getResp, err := server.GetUser(context.Background(), getReq)
	require.NoError(t, err)
	require.Equal(t, "alice@example.com", getResp.GetUser().GetEmail())

	updateReq := new(userv1.UpdateEmailRequest)
	updateReq.SetUserId(getResp.GetUser().GetUserId())
	updateReq.SetEmail("Bob@Example.com")
	updateResp, err := server.UpdateEmail(context.Background(), updateReq)
	require.NoError(t, err)
	require.Equal(t, "bob@example.com", updateResp.GetUser().GetEmail())
}

func TestUpdateEmailSameAddressKeepsVerification(t *testing.T) {
	store := newFakeStore()
	store.user = &model.User{
		UserID:          1001,
		Email:           "same@example.com",
		EmailVerifiedAt: 4001,
	}
	server := newTestUserServer(t, store)

	req := new(userv1.UpdateEmailRequest)
	req.SetUserId(1001)
	req.SetEmail("Same@Example.com")
	resp, err := server.UpdateEmail(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "same@example.com", resp.GetUser().GetEmail())
	require.Equal(t, int64(4001), resp.GetUser().GetEmailVerifiedAt())
}

func TestCreateUserInvalidUsername(t *testing.T) {
	server := newTestUserServer(t, newFakeStore())

	tests := []struct {
		name     string
		username string
	}{
		{"empty", ""},
		{"too short", "a"},
		{"has space", "has space"},
		{"has emoji", "emoji😀"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := new(userv1.CreateUserRequest)
			req.SetName("display name")
			req.SetEmail("user@example.com")
			req.SetUsername(tt.username)
			_, err := server.CreateUser(context.Background(), req)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func TestCreateUserUsernameNormalized(t *testing.T) {
	store := newFakeStore()
	server := newTestUserServer(t, store)

	req := new(userv1.CreateUserRequest)
	req.SetName("display name")
	req.SetEmail("user@example.com")
	req.SetUsername("  MyName_1  ")

	resp, err := server.CreateUser(context.Background(), req)
	require.NoError(t, err)
	require.NotZero(t, resp.GetUser().GetUserId())
	require.Equal(t, "myname_1", store.profile.Username)
}

func TestCreateUserUsernameTaken(t *testing.T) {
	store := newFakeStore()
	store.createProfileErr = &pq.Error{Code: "23505", Constraint: "user_profiles_username_active_idx"}
	server := newTestUserServer(t, store)

	req := new(userv1.CreateUserRequest)
	req.SetName("display name")
	req.SetEmail("user@example.com")
	req.SetUsername("tester")

	_, err := server.CreateUser(context.Background(), req)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.UserDomain, rpcerror.UserUsernameTaken))
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
	server := newTestUserServer(t, store)

	req := new(userv1.UpdateUsernameRequest)
	req.SetUserId(1001)
	req.SetUsername("  New_Name42  ")
	resp, err := server.UpdateUsername(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "new_name42", resp.GetProfile().GetUsername())
	require.Equal(t, "new_name42", store.profile.Username)
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
	store.updateUsernameErr = &pq.Error{Code: "23505", Constraint: "user_profiles_username_active_idx"}
	_, err = server.UpdateUsername(context.Background(), req)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.UserDomain, rpcerror.UserUsernameTaken))

	store.updateUsernameErr = nil
	req.SetUserId(9999)
	_, err = server.UpdateUsername(context.Background(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
}
