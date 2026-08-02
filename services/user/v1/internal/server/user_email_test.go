package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/services/user/v1/internal/model"
)

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
