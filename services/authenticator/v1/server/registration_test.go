package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/pkg/mail"
	"github.com/soasurs/cordis/pkg/password"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/authenticator/v1/config"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/token"
)

func TestRegister(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	userClient := &fakeUserClient{
		createUserResponse: createUserResponse(1001, "user@example.com"),
	}
	server := newTestAuthenticatorServer(t, store, tokens, userClient)
	mailerClient := new(fakeMailerClient)
	server.(*authenticatorServer).svcCtx.MailerClient = mailerClient
	limiter := new(recordingPasswordLimiter)
	server.(*authenticatorServer).svcCtx.PasswordLimiter = limiter

	req := new(authenticatorv1.RegisterRequest)
	req.SetName("  display name  ")
	req.SetUsername("tester")
	req.SetEmail("user@example.com")
	req.SetPassword("password")

	resp, err := server.Register(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "display name", userClient.createUserRequest.GetName())
	require.Equal(t, "user@example.com", userClient.createUserRequest.GetEmail())
	require.NotNil(t, store.credentials[1001])
	match, err := password.Verify(store.credentials[1001].HashedPassword, "password")
	require.NoError(t, err)
	require.True(t, match)
	require.True(t, resp.GetOk())
	require.Nil(t, store.createdSession)
	delivered := mailerClient.onlyMail(t)
	require.Equal(t, mail.TemplateEmailVerification, delivered.template)
	verification := store.emailVerifications[token.Hash(delivered.token)]
	require.NotNil(t, verification)
	require.Equal(t, int64(1001), verification.UserID)
	require.Equal(t, "user@example.com", verification.Email)
	calls, releases := limiter.snapshot()
	require.Equal(t, 1, calls)
	require.Equal(t, 1, releases)
}

func TestRegisterInviteOnlyRedeemsInvite(t *testing.T) {
	sessionStore := newFakeSessionStore()
	const rawCode = "registration-invite"
	sessionStore.registrationInvites[token.Hash(rawCode)] = &model.RegistrationInvite{
		ID:         3001,
		CodeHash:   token.Hash(rawCode),
		BoundEmail: "user@example.com",
		CreatedAt:  time.Now().UnixMilli(),
		ExpiresAt:  time.Now().Add(time.Hour).UnixMilli(),
	}
	userClient := &fakeUserClient{
		createUserResponse: createUserResponse(1001, "user@example.com"),
	}
	server := newTestAuthenticatorServer(t, sessionStore, newTestTokenManager(t), userClient)
	server.(*authenticatorServer).svcCtx.Cfg.Registration = config.RegistrationConfig{
		Mode:           config.RegistrationModeInviteOnly,
		ReservationTTL: time.Minute,
	}

	req := new(authenticatorv1.RegisterRequest)
	req.SetName("display name")
	req.SetUsername("tester")
	req.SetEmail(" User@Example.COM ")
	req.SetPassword("password")
	req.SetRegistrationInviteCode(rawCode)

	resp, err := server.Register(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	invite := sessionStore.registrationInvites[token.Hash(rawCode)]
	require.Equal(t, "user@example.com", invite.ReservedEmail)
	require.Equal(t, int64(1001), invite.RedeemedUserID)
	require.NotZero(t, invite.RedeemedAt)
	require.Equal(t, "user@example.com", userClient.createUserRequest.GetEmail())
}

func TestRegisterInviteOnlyRejectsUnavailableInvite(t *testing.T) {
	userClient := new(fakeUserClient)
	server := newTestAuthenticatorServer(t, newFakeSessionStore(), newTestTokenManager(t), userClient)
	server.(*authenticatorServer).svcCtx.Cfg.Registration = config.RegistrationConfig{
		Mode:           config.RegistrationModeInviteOnly,
		ReservationTTL: time.Minute,
	}

	req := new(authenticatorv1.RegisterRequest)
	req.SetName("display name")
	req.SetUsername("tester")
	req.SetEmail("user@example.com")
	req.SetPassword("password")
	req.SetRegistrationInviteCode("missing")

	_, err := server.Register(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidRegistrationInvite))
	require.Nil(t, userClient.createUserRequest)
}

func TestRegisterClosedStopsBeforePasswordHashAndUserRPC(t *testing.T) {
	userClient := new(fakeUserClient)
	server := newTestAuthenticatorServer(t, newFakeSessionStore(), newTestTokenManager(t), userClient)
	limiter := new(recordingPasswordLimiter)
	server.(*authenticatorServer).svcCtx.PasswordLimiter = limiter
	server.(*authenticatorServer).svcCtx.Cfg.Registration.Mode = config.RegistrationModeClosed

	req := new(authenticatorv1.RegisterRequest)
	req.SetName("display name")
	req.SetUsername("tester")
	req.SetEmail("user@example.com")
	req.SetPassword("password")

	_, err := server.Register(t.Context(), req)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorRegistrationClosed))
	require.Nil(t, userClient.createUserRequest)
	calls, _ := limiter.snapshot()
	require.Zero(t, calls)
}

func TestRegisterUserError(t *testing.T) {
	expectedErr := rpcerror.New(codes.AlreadyExists, rpcerror.UserDomain, rpcerror.UserEmailAlreadyExists, "email already exists")
	server := newTestAuthenticatorServer(t, newFakeSessionStore(), newTestTokenManager(t), &fakeUserClient{
		createUserErr: expectedErr,
	})

	req := new(authenticatorv1.RegisterRequest)
	req.SetName("display name")
	req.SetUsername("tester")
	req.SetEmail("user@example.com")
	req.SetPassword("password")

	_, err := server.Register(context.Background(), req)
	require.ErrorIs(t, err, expectedErr)
}

func TestRegisterPasswordLimiterErrorStopsBeforeUserRPC(t *testing.T) {
	userClient := new(fakeUserClient)
	server := newTestAuthenticatorServer(t, newFakeSessionStore(), newTestTokenManager(t), userClient)
	server.(*authenticatorServer).svcCtx.PasswordLimiter = &recordingPasswordLimiter{err: context.Canceled}

	req := new(authenticatorv1.RegisterRequest)
	req.SetName("display name")
	req.SetUsername("tester")
	req.SetEmail("user@example.com")
	req.SetPassword("password")
	_, err := server.Register(t.Context(), req)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, userClient.createUserRequest)
}
