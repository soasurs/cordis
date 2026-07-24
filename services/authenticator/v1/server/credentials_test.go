package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/pkg/password"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/token"
)

func TestChangePasswordReplacesCredentialAndRevokesOtherSessions(t *testing.T) {
	store := newFakeSessionStore()
	seedCredential(t, store, 1001, "old-password")
	server := newTestAuthenticatorServer(t, store, newTestTokenManager(t), &fakeUserClient{})

	req := new(authenticatorv1.ChangePasswordRequest)
	req.SetUserId(1001)
	req.SetCurrentSessionId(2001)
	req.SetOldPassword("old-password")
	req.SetNewPassword("new-password")
	resp, err := server.ChangePassword(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())

	match, err := password.Verify(store.credentials[1001].HashedPassword, "new-password")
	require.NoError(t, err)
	require.True(t, match)
	require.Equal(t, int64(1001), store.revokedOtherUserID)
	require.Equal(t, int64(2001), store.currentSessionID)
}

func TestChangePasswordWrongOldPasswordChangesNothing(t *testing.T) {
	store := newFakeSessionStore()
	seedCredential(t, store, 1001, "old-password")
	previousHash := store.credentials[1001].HashedPassword
	server := newTestAuthenticatorServer(t, store, newTestTokenManager(t), &fakeUserClient{})

	req := new(authenticatorv1.ChangePasswordRequest)
	req.SetUserId(1001)
	req.SetCurrentSessionId(2001)
	req.SetOldPassword("wrong-password")
	req.SetNewPassword("new-password")
	resp, err := server.ChangePassword(context.Background(), req)
	require.NoError(t, err)
	require.False(t, resp.GetOk())
	require.Equal(t, previousHash, store.credentials[1001].HashedPassword)
	require.Zero(t, store.revokedOtherUserID)
}

func TestChangePasswordMissingCredential(t *testing.T) {
	server := newTestAuthenticatorServer(t, newFakeSessionStore(), newTestTokenManager(t), &fakeUserClient{})

	req := new(authenticatorv1.ChangePasswordRequest)
	req.SetUserId(1001)
	req.SetOldPassword("old-password")
	req.SetNewPassword("new-password")
	_, err := server.ChangePassword(context.Background(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestRegisterClaimsHalfRegisteredAccount(t *testing.T) {
	store := newFakeSessionStore()
	userClient := &fakeUserClient{
		createUserErr:   rpcerror.New(codes.AlreadyExists, rpcerror.UserDomain, rpcerror.UserEmailAlreadyExists, "email already exists"),
		getUserResponse: userResponse(1001, "user@example.com"),
	}
	server := newTestAuthenticatorServer(t, store, newTestTokenManager(t), userClient)

	// The user row exists but no credential was ever stored: the earlier
	// registration died between the two writes and the account never worked.
	req := new(authenticatorv1.RegisterRequest)
	req.SetName("display name")
	req.SetUsername("tester")
	req.SetEmail("user@example.com")
	req.SetPassword("password")
	resp, err := server.Register(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())

	match, err := password.Verify(store.credentials[1001].HashedPassword, "password")
	require.NoError(t, err)
	require.True(t, match)
}

func TestRegisterDuplicateEmailKeepsAlreadyExists(t *testing.T) {
	store := newFakeSessionStore()
	seedCredential(t, store, 1001, "existing-password")
	expectedErr := rpcerror.New(codes.AlreadyExists, rpcerror.UserDomain, rpcerror.UserEmailAlreadyExists, "email already exists")
	server := newTestAuthenticatorServer(t, store, newTestTokenManager(t), &fakeUserClient{
		createUserErr:   expectedErr,
		getUserResponse: userResponse(1001, "user@example.com"),
	})

	req := new(authenticatorv1.RegisterRequest)
	req.SetName("display name")
	req.SetUsername("tester")
	req.SetEmail("user@example.com")
	req.SetPassword("other-password")
	_, err := server.Register(context.Background(), req)
	require.Equal(t, codes.AlreadyExists, status.Code(err))

	// The complete account keeps its original credential.
	match, verifyErr := password.Verify(store.credentials[1001].HashedPassword, "existing-password")
	require.NoError(t, verifyErr)
	require.True(t, match)
}

func TestLoginUnknownEmail(t *testing.T) {
	server := newTestAuthenticatorServer(t, newFakeSessionStore(), newTestTokenManager(t), &fakeUserClient{
		getUserErr: status.Error(codes.NotFound, "resource not found"),
	})

	req := new(authenticatorv1.LoginRequest)
	req.SetEmail("unknown@example.com")
	req.SetPassword("password")
	_, err := server.Login(context.Background(), req)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidCredentials))
}

func TestConfirmPasswordResetRejectsHalfRegisteredAccount(t *testing.T) {
	sessionStore := newFakeSessionStore()
	now := time.Now().UnixMilli()
	sessionStore.passwordResets[token.Hash("claim-token")] = &model.PasswordResetToken{
		UserID:    1001,
		TokenHash: token.Hash("claim-token"),
		CreatedAt: now,
		ExpiresAt: now + 60_000,
	}
	server := newRecoveryTestServer(t, sessionStore, &fakeUserClient{}, new(fakeMailerClient))

	req := new(authenticatorv1.ConfirmPasswordResetRequest)
	req.SetToken("claim-token")
	req.SetNewPassword("recovered-password")
	resp, err := server.ConfirmPasswordReset(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidPasswordResetToken))
	require.Nil(t, resp)
	require.Nil(t, sessionStore.credentials[1001])
	require.Zero(t, sessionStore.passwordResets[token.Hash("claim-token")].ConsumedAt)
}
