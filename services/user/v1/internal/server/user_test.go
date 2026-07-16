package server

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/lib/pq"
	"github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/password"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/user/v1/internal/model"
	"github.com/soasurs/cordis/services/user/v1/internal/store"
	"github.com/soasurs/cordis/services/user/v1/internal/svc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCreateUser(t *testing.T) {
	store := newFakeStore()
	server := newTestUserServer(t, store)

	req := new(userv1.CreateUserRequest)
	req.SetName("display name")
	req.SetEmail("user@example.com")
	req.SetPassword("password")

	resp, err := server.CreateUser(context.Background(), req)
	require.NoError(t, err)
	require.NotZero(t, resp.GetUser().GetUserId())
	require.Equal(t, "user@example.com", resp.GetUser().GetEmail())
	require.NotNil(t, store.profile)
	require.Equal(t, "display name", store.profile.Name)
	require.NotEmpty(t, store.user.HashedPassword)
	require.NotEqual(t, "password", store.user.HashedPassword)
}

func TestCreateUserValidation(t *testing.T) {
	server := newTestUserServer(t, newFakeStore())

	req := new(userv1.CreateUserRequest)
	req.SetEmail("user@example.com")
	req.SetPassword("password")

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
	req.SetPassword("password")

	_, err := server.CreateUser(context.Background(), req)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.UserDomain, rpcerror.UserEmailAlreadyExists))
}

func TestVerifyPassword(t *testing.T) {
	hashedPassword, err := password.Hash("password")
	require.NoError(t, err)

	store := newFakeStore()
	store.user = &model.User{
		UserID:         1001,
		Email:          "user@example.com",
		HashedPassword: hashedPassword,
	}
	server := newTestUserServer(t, store)

	req := new(userv1.VerifyPasswordRequest)
	req.SetEmail("user@example.com")
	req.SetPassword("password")

	resp, err := server.VerifyPassword(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Equal(t, int64(1001), resp.GetUserId())
}

func TestVerifyPasswordMismatch(t *testing.T) {
	hashedPassword, err := password.Hash("password")
	require.NoError(t, err)

	store := newFakeStore()
	store.user = &model.User{
		UserID:         1001,
		Email:          "user@example.com",
		HashedPassword: hashedPassword,
	}
	server := newTestUserServer(t, store)

	req := new(userv1.VerifyPasswordRequest)
	req.SetEmail("user@example.com")
	req.SetPassword("wrong-password")

	resp, err := server.VerifyPassword(context.Background(), req)
	require.NoError(t, err)
	require.False(t, resp.GetOk())
	require.Zero(t, resp.GetUserId())
}

func TestVerifyPasswordUnknownEmail(t *testing.T) {
	store := newFakeStore()
	store.getUserWithEmailErr = sql.ErrNoRows
	server := newTestUserServer(t, store)

	req := new(userv1.VerifyPasswordRequest)
	req.SetEmail("missing@example.com")
	req.SetPassword("password")

	resp, err := server.VerifyPassword(context.Background(), req)
	require.NoError(t, err)
	require.False(t, resp.GetOk())
	require.Zero(t, resp.GetUserId())
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
		UserID:    1001,
		Name:      "display name",
		AvatarURI: "avatar://1",
	}
	server := newTestUserServer(t, store)

	req := new(userv1.GetUserProfileRequest)
	req.SetUserId(1001)

	resp, err := server.GetUserProfile(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), resp.GetProfile().GetUserId())
	require.Equal(t, "display name", resp.GetProfile().GetName())
	require.Equal(t, "avatar://1", resp.GetProfile().GetAvatarUri())
}

func TestUpdateUserProfile(t *testing.T) {
	store := newFakeStore()
	store.profile = &model.UserProfile{
		UserID:    1001,
		Name:      "old name",
		AvatarURI: "avatar://1",
	}
	server := newTestUserServer(t, store)

	req := new(userv1.UpdateUserProfileRequest)
	req.SetUserId(1001)
	req.SetName(" new name ")
	req.SetAvatarUri("avatar://2")

	resp, err := server.UpdateUserProfile(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "new name", resp.GetProfile().GetName())
	require.Equal(t, "avatar://2", resp.GetProfile().GetAvatarUri())
}

func TestUpdateUserProfileValidation(t *testing.T) {
	server := newTestUserServer(t, newFakeStore())

	req := new(userv1.UpdateUserProfileRequest)
	req.SetUserId(1001)

	_, err := server.UpdateUserProfile(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestChangePassword(t *testing.T) {
	hashedPassword, err := password.Hash("old-password")
	require.NoError(t, err)

	store := newFakeStore()
	store.user = &model.User{
		UserID:         1001,
		Email:          "user@example.com",
		HashedPassword: hashedPassword,
	}
	server := newTestUserServer(t, store)

	req := new(userv1.ChangePasswordRequest)
	req.SetUserId(1001)
	req.SetOldPassword("old-password")
	req.SetNewPassword("new-password")

	resp, err := server.ChangePassword(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.NotEmpty(t, store.updatedPasswordHash)
	require.NotEqual(t, "new-password", store.updatedPasswordHash)
}

func TestChangePasswordMismatch(t *testing.T) {
	hashedPassword, err := password.Hash("old-password")
	require.NoError(t, err)

	store := newFakeStore()
	store.user = &model.User{
		UserID:         1001,
		Email:          "user@example.com",
		HashedPassword: hashedPassword,
	}
	server := newTestUserServer(t, store)

	req := new(userv1.ChangePasswordRequest)
	req.SetUserId(1001)
	req.SetOldPassword("wrong-password")
	req.SetNewPassword("new-password")

	resp, err := server.ChangePassword(context.Background(), req)
	require.NoError(t, err)
	require.False(t, resp.GetOk())
	require.Empty(t, store.updatedPasswordHash)
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
	req.SetPassword("password")

	_, err := server.CreateUser(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestCreateUserInvalidEmail(t *testing.T) {
	server := newTestUserServer(t, newFakeStore())

	req := new(userv1.CreateUserRequest)
	req.SetName("name")
	req.SetEmail("no-at-sign")
	req.SetPassword("password")

	_, err := server.CreateUser(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestChangePasswordEmptyNewPassword(t *testing.T) {
	hashedPassword, _ := password.Hash("old-password")
	store := newFakeStore()
	store.user = &model.User{UserID: 1001, HashedPassword: hashedPassword}
	server := newTestUserServer(t, store)

	req := new(userv1.ChangePasswordRequest)
	req.SetUserId(1001)
	req.SetOldPassword("old-password")

	_, err := server.ChangePassword(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGetUserNotFound(t *testing.T) {
	server := newTestUserServer(t, newFakeStore())

	req := new(userv1.GetUserRequest)
	req.SetUserId(9999)

	_, err := server.GetUser(context.Background(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func newTestUserServer(t *testing.T, store store.Store) userv1.UserServiceServer {
	t.Helper()

	node, err := snowflake.New()
	require.NoError(t, err)

	return New(&svc.ServiceContext{
		Store:     store,
		Snowflake: node,
	})
}

type fakeStore struct {
	user                *model.User
	profile             *model.UserProfile
	createUserErr       error
	getUserWithEmailErr error
	emailAvailable      bool
	updatedPasswordHash string
}

func newFakeStore() *fakeStore {
	return &fakeStore{}
}

func (s *fakeStore) Transact(ctx context.Context, fn func(txStore store.Store) error) error {
	return fn(s)
}

func (s *fakeStore) CreateUser(_ context.Context, userID int64, email, hashedPassword string) (*model.User, error) {
	if s.createUserErr != nil {
		return nil, s.createUserErr
	}
	s.user = &model.User{
		UserID:         userID,
		Email:          email,
		HashedPassword: hashedPassword,
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

func (s *fakeStore) UpdateUserPassword(_ context.Context, userID int64, hashedPassword string) error {
	if s.user == nil || s.user.UserID != userID {
		return sql.ErrNoRows
	}
	s.updatedPasswordHash = hashedPassword
	s.user.HashedPassword = hashedPassword
	return nil
}

func (s *fakeStore) UpdateUserEmail(_ context.Context, userID int64, email string) (*model.User, error) {
	if s.user == nil || s.user.UserID != userID {
		return nil, sql.ErrNoRows
	}
	s.user.Email = email
	return s.user, nil
}

func (s *fakeStore) CreateUserProfile(_ context.Context, userID int64, name, avatarURI string) (*model.UserProfile, error) {
	if s.user == nil || s.user.UserID != userID {
		return nil, errors.New("missing user")
	}
	s.profile = &model.UserProfile{
		UserID:    userID,
		Name:      name,
		AvatarURI: avatarURI,
	}
	return s.profile, nil
}

func (s *fakeStore) GetUserProfile(_ context.Context, userID int64) (*model.UserProfile, error) {
	if s.profile == nil || s.profile.UserID != userID {
		return nil, sql.ErrNoRows
	}
	return s.profile, nil
}

func (s *fakeStore) UpdateUserProfile(_ context.Context, userID int64, name, avatarURI string) (*model.UserProfile, error) {
	if s.profile == nil || s.profile.UserID != userID {
		return nil, sql.ErrNoRows
	}
	s.profile.Name = name
	s.profile.AvatarURI = avatarURI
	return s.profile, nil
}
