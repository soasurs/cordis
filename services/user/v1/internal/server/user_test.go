package server

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/lib/pq"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/password"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/user/v1/internal/model"
	"github.com/soasurs/cordis/services/user/v1/internal/store"
	"github.com/soasurs/cordis/services/user/v1/internal/svc"
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
	if err != nil {
		t.Fatalf("CreateUser returned error: %v", err)
	}
	if resp.GetUser().GetUserId() == 0 {
		t.Fatal("expected user id")
	}
	if resp.GetUser().GetEmail() != "user@example.com" {
		t.Fatalf("email = %q, want user@example.com", resp.GetUser().GetEmail())
	}
	if store.profile == nil || store.profile.Name != "display name" {
		t.Fatalf("expected profile creation, got %+v", store.profile)
	}
	if store.user.HashedPassword == "" || store.user.HashedPassword == "password" {
		t.Fatalf("expected hashed password, got %q", store.user.HashedPassword)
	}
}

func TestCreateUserValidation(t *testing.T) {
	server := newTestUserServer(t, newFakeStore())

	req := new(userv1.CreateUserRequest)
	req.SetEmail("user@example.com")
	req.SetPassword("password")

	_, err := server.CreateUser(context.Background(), req)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("CreateUser code = %v, want %v: %v", status.Code(err), codes.InvalidArgument, err)
	}
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
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("CreateUser code = %v, want %v: %v", status.Code(err), codes.AlreadyExists, err)
	}
	if !rpcerror.Is(err, rpcerror.UserDomain, rpcerror.UserEmailAlreadyExists) {
		t.Fatalf("expected email already exists reason: %v", err)
	}
}

func TestVerifyPassword(t *testing.T) {
	hashedPassword, err := password.Hash("password")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

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
	if err != nil {
		t.Fatalf("VerifyPassword returned error: %v", err)
	}
	if !resp.GetOk() || resp.GetUserId() != 1001 {
		t.Fatalf("unexpected response: %v", resp)
	}
}

func TestVerifyPasswordMismatch(t *testing.T) {
	hashedPassword, err := password.Hash("password")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

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
	if err != nil {
		t.Fatalf("VerifyPassword returned error: %v", err)
	}
	if resp.GetOk() || resp.GetUserId() != 0 {
		t.Fatalf("unexpected response: %v", resp)
	}
}

func TestVerifyPasswordUnknownEmail(t *testing.T) {
	store := newFakeStore()
	store.getUserWithEmailErr = sql.ErrNoRows
	server := newTestUserServer(t, store)

	req := new(userv1.VerifyPasswordRequest)
	req.SetEmail("missing@example.com")
	req.SetPassword("password")

	resp, err := server.VerifyPassword(context.Background(), req)
	if err != nil {
		t.Fatalf("VerifyPassword returned error: %v", err)
	}
	if resp.GetOk() || resp.GetUserId() != 0 {
		t.Fatalf("unexpected response: %v", resp)
	}
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
	if err != nil {
		t.Fatalf("GetUser returned error: %v", err)
	}
	if resp.GetUser().GetUserId() != 1001 || resp.GetUser().GetEmail() != "user@example.com" {
		t.Fatalf("unexpected response: %v", resp)
	}
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
	if err != nil {
		t.Fatalf("GetUser returned error: %v", err)
	}
	if resp.GetUser().GetUserId() != 1001 || resp.GetUser().GetEmail() != "user@example.com" {
		t.Fatalf("unexpected response: %v", resp)
	}
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
	if err != nil {
		t.Fatalf("GetUserProfile returned error: %v", err)
	}
	if resp.GetProfile().GetUserId() != 1001 ||
		resp.GetProfile().GetName() != "display name" ||
		resp.GetProfile().GetAvatarUri() != "avatar://1" {
		t.Fatalf("unexpected response: %v", resp)
	}
}

func TestChangePassword(t *testing.T) {
	hashedPassword, err := password.Hash("old-password")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

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
	if err != nil {
		t.Fatalf("ChangePassword returned error: %v", err)
	}
	if !resp.GetOk() {
		t.Fatalf("unexpected response: %v", resp)
	}
	if store.updatedPasswordHash == "" || store.updatedPasswordHash == "new-password" {
		t.Fatalf("expected stored password hash, got %q", store.updatedPasswordHash)
	}
}

func TestChangePasswordMismatch(t *testing.T) {
	hashedPassword, err := password.Hash("old-password")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

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
	if err != nil {
		t.Fatalf("ChangePassword returned error: %v", err)
	}
	if resp.GetOk() || store.updatedPasswordHash != "" {
		t.Fatalf("unexpected response=%v updated_hash=%q", resp, store.updatedPasswordHash)
	}
}

func TestCheckEmailAvailability(t *testing.T) {
	store := newFakeStore()
	store.emailAvailable = true
	server := newTestUserServer(t, store)

	req := new(userv1.CheckEmailAvailabilityRequest)
	req.SetEmail("user@example.com")

	resp, err := server.CheckEmailAvailability(context.Background(), req)
	if err != nil {
		t.Fatalf("CheckEmailAvailability returned error: %v", err)
	}
	if !resp.GetAvailable() {
		t.Fatalf("unexpected response: %v", resp)
	}
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
	if err != nil {
		t.Fatalf("UpdateEmail returned error: %v", err)
	}
	if resp.GetUser().GetEmail() != "new@example.com" {
		t.Fatalf("unexpected response: %v", resp)
	}
}

func newTestUserServer(t *testing.T, store store.Store) userv1.UserServiceServer {
	t.Helper()

	node, err := snowflake.New()
	if err != nil {
		t.Fatalf("new snowflake node: %v", err)
	}

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
