package server

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/user/v1/internal/model"
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
	store.createUserErr = &pgconn.PgError{Code: "23505"}
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

func TestCheckUsernameAvailability(t *testing.T) {
	store := newFakeStore()
	store.usernameAvailable = true
	server := newTestUserServer(t, store)

	req := new(userv1.CheckUsernameAvailabilityRequest)
	req.SetUsername(" Free_Handle ")
	resp, err := server.CheckUsernameAvailability(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.GetAvailable())

	store.usernameAvailable = false
	resp, err = server.CheckUsernameAvailability(context.Background(), req)
	require.NoError(t, err)
	require.False(t, resp.GetAvailable())

	invalid := new(userv1.CheckUsernameAvailabilityRequest)
	invalid.SetUsername("Bad Handle!")
	_, err = server.CheckUsernameAvailability(context.Background(), invalid)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestCreateUserNameTooLong(t *testing.T) {
	server := newTestUserServer(t, newFakeStore())

	req := new(userv1.CreateUserRequest)
	req.SetName(strings.Repeat("刺", maxNameRunes+1))
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
	store.createProfileErr = &pgconn.PgError{Code: "23505", ConstraintName: "user_profiles_username_active_idx"}
	server := newTestUserServer(t, store)

	req := new(userv1.CreateUserRequest)
	req.SetName("display name")
	req.SetEmail("user@example.com")
	req.SetUsername("tester")

	_, err := server.CreateUser(context.Background(), req)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.UserDomain, rpcerror.UserUsernameTaken))
}
