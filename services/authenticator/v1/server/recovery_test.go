package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	mailerv1 "github.com/soasurs/cordis/gen/mailer/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/mail"
	"github.com/soasurs/cordis/pkg/password"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/authenticator/v1/config"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/store"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/token"
	"github.com/soasurs/cordis/services/authenticator/v1/svc"
)

type sentMail struct {
	to       string
	template string
	token    string
}

type fakeMailerClient struct {
	mailerv1.MailerServiceClient
	sent []sentMail
	err  error
}

func (c *fakeMailerClient) SendEmail(_ context.Context, req *mailerv1.SendEmailRequest, _ ...grpc.CallOption) (*mailerv1.SendEmailResponse, error) {
	c.sent = append(c.sent, sentMail{
		to:       req.GetTo(),
		template: req.GetTemplate(),
		token:    req.GetVariables()[mail.VariableToken],
	})
	if c.err != nil {
		return nil, c.err
	}
	resp := new(mailerv1.SendEmailResponse)
	resp.SetOk(true)
	return resp, nil
}

func (c *fakeMailerClient) onlyMail(t *testing.T) sentMail {
	t.Helper()
	require.Len(t, c.sent, 1)
	return c.sent[0]
}

func newRecoveryTestServer(
	t *testing.T,
	sessionStore store.Store,
	userClient userv1.UserServiceClient,
	mailerClient mailerv1.MailerServiceClient,
) authenticatorv1.AuthenticatorServiceServer {
	t.Helper()
	return newThrottledRecoveryTestServer(t, sessionStore, userClient, mailerClient, nil)
}

func newThrottledRecoveryTestServer(
	t *testing.T,
	sessionStore store.Store,
	userClient userv1.UserServiceClient,
	mailerClient mailerv1.MailerServiceClient,
	limiter svc.RecoveryLimiter,
) authenticatorv1.AuthenticatorServiceServer {
	t.Helper()

	node, err := snowflake.New()
	require.NoError(t, err)
	tokens, err := token.NewManager(token.Config{
		Issuer:        "cordis.test",
		AccessSecret:  "access-secret-0123456789abcdef-0123",
		RefreshSecret: "refresh-secret-0123456789abcdef-012",
		AccessTTL:     time.Minute,
		RefreshTTL:    time.Hour,
	})
	require.NoError(t, err)

	return New(&svc.ServiceContext{
		Cfg: config.Config{
			Sessions: config.SessionConfig{TTL: time.Hour},
			Recovery: config.RecoveryConfig{
				PasswordResetTTL:     30 * time.Minute,
				EmailVerificationTTL: 24 * time.Hour,
			},
		},
		Store:           sessionStore,
		Tokens:          tokens,
		TwoFactor:       newTestTwoFactorCipher(t),
		Snowflake:       node,
		UserClient:      userClient,
		MailerClient:    mailerClient,
		RecoveryLimiter: limiter,
	})
}

func recoveryTestUser(userID int64, email string, verifiedAt int64) *userv1.GetUserResponse {
	user := new(userv1.User)
	user.SetUserId(userID)
	user.SetEmail(email)
	user.SetEmailVerifiedAt(verifiedAt)
	resp := new(userv1.GetUserResponse)
	resp.SetUser(user)
	return resp
}

func TestRequestPasswordResetStoresTokenAndSendsMail(t *testing.T) {
	sessionStore := newFakeSessionStore()
	seedCredential(t, sessionStore, 1001, "old-password")
	mailerClient := new(fakeMailerClient)
	server := newRecoveryTestServer(t, sessionStore, &fakeUserClient{
		getUserResponse: recoveryTestUser(1001, "user@example.com", 0),
	}, mailerClient)

	req := new(authenticatorv1.RequestPasswordResetRequest)
	req.SetEmail("user@example.com")
	resp, err := server.RequestPasswordReset(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())

	delivered := mailerClient.onlyMail(t)
	require.Equal(t, "user@example.com", delivered.to)
	require.Equal(t, mail.TemplatePasswordReset, delivered.template)
	require.NotEmpty(t, delivered.token)

	stored := sessionStore.passwordResets[token.Hash(delivered.token)]
	require.NotNil(t, stored)
	require.Equal(t, int64(1001), stored.UserID)
	require.Greater(t, stored.ExpiresAt, time.Now().UnixMilli())

	// A new request replaces the previous token.
	_, err = server.RequestPasswordReset(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, sessionStore.passwordResets, 1)
	require.Nil(t, sessionStore.passwordResets[token.Hash(delivered.token)])
}

func TestRequestPasswordResetUnknownEmailReturnsOk(t *testing.T) {
	sessionStore := newFakeSessionStore()
	mailerClient := new(fakeMailerClient)
	server := newRecoveryTestServer(t, sessionStore, &fakeUserClient{
		getUserErr: status.Error(codes.NotFound, "user not found"),
	}, mailerClient)

	req := new(authenticatorv1.RequestPasswordResetRequest)
	req.SetEmail("unknown@example.com")
	resp, err := server.RequestPasswordReset(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Empty(t, mailerClient.sent)
	require.Empty(t, sessionStore.passwordResets)
}

func TestRequestPasswordResetHalfRegisteredAccountReturnsOkWithoutToken(t *testing.T) {
	sessionStore := newFakeSessionStore()
	mailerClient := new(fakeMailerClient)
	server := newRecoveryTestServer(t, sessionStore, &fakeUserClient{
		getUserResponse: recoveryTestUser(1001, "user@example.com", 0),
	}, mailerClient)

	req := new(authenticatorv1.RequestPasswordResetRequest)
	req.SetEmail("user@example.com")
	resp, err := server.RequestPasswordReset(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Empty(t, mailerClient.sent)
	require.Empty(t, sessionStore.passwordResets)
}

func TestRequestPasswordResetValidation(t *testing.T) {
	server := newRecoveryTestServer(t, newFakeSessionStore(), &fakeUserClient{}, new(fakeMailerClient))

	req := new(authenticatorv1.RequestPasswordResetRequest)
	_, err := server.RequestPasswordReset(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	req.SetEmail("not-an-email")
	_, err = server.RequestPasswordReset(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestRequestPasswordResetWithoutMailerStillSucceeds(t *testing.T) {
	sessionStore := newFakeSessionStore()
	seedCredential(t, sessionStore, 1001, "old-password")
	server := newRecoveryTestServer(t, sessionStore, &fakeUserClient{
		getUserResponse: recoveryTestUser(1001, "user@example.com", 0),
	}, nil)

	req := new(authenticatorv1.RequestPasswordResetRequest)
	req.SetEmail("user@example.com")
	resp, err := server.RequestPasswordReset(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Len(t, sessionStore.passwordResets, 1)
}

func TestConfirmPasswordResetSuccess(t *testing.T) {
	sessionStore := newFakeSessionStore()
	seedCredential(t, sessionStore, 1001, "old-password")
	now := time.Now().UnixMilli()
	sessionStore.passwordResets[token.Hash("raw-reset-token")] = &model.PasswordResetToken{
		UserID:    1001,
		TokenHash: token.Hash("raw-reset-token"),
		CreatedAt: now,
		ExpiresAt: now + 60_000,
	}
	server := newRecoveryTestServer(t, sessionStore, &fakeUserClient{}, new(fakeMailerClient))
	limiter := new(recordingPasswordLimiter)
	server.(*authenticatorServer).svcCtx.PasswordLimiter = limiter

	req := new(authenticatorv1.ConfirmPasswordResetRequest)
	req.SetToken("raw-reset-token")
	req.SetNewPassword("new-password")
	resp, err := server.ConfirmPasswordReset(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())

	// The credential is replaced locally, atomically with the consume.
	require.NotNil(t, sessionStore.credentials[1001])
	match, err := password.Verify(sessionStore.credentials[1001].HashedPassword, "new-password")
	require.NoError(t, err)
	require.True(t, match)
	require.NotZero(t, sessionStore.passwordResets[token.Hash("raw-reset-token")].ConsumedAt)
	// All sessions are revoked: currentSessionID zero matches none.
	require.Equal(t, int64(1001), sessionStore.revokedOtherUserID)
	require.Zero(t, sessionStore.currentSessionID)
	require.Equal(t, []bool{false, true}, sessionStore.passwordResetReads)
	calls, releases := limiter.snapshot()
	require.Equal(t, 1, calls)
	require.Equal(t, 1, releases)

	// The consumed token cannot be replayed.
	_, err = server.ConfirmPasswordReset(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidPasswordResetToken))
	calls, releases = limiter.snapshot()
	require.Equal(t, 1, calls)
	require.Equal(t, 1, releases)
}

func TestConfirmPasswordResetRejectsUnknownAndExpired(t *testing.T) {
	sessionStore := newFakeSessionStore()
	now := time.Now().UnixMilli()
	sessionStore.passwordResets[token.Hash("expired-token")] = &model.PasswordResetToken{
		UserID:    1001,
		TokenHash: token.Hash("expired-token"),
		CreatedAt: now - 120_000,
		ExpiresAt: now - 60_000,
	}
	server := newRecoveryTestServer(t, sessionStore, &fakeUserClient{}, new(fakeMailerClient))
	limiter := new(recordingPasswordLimiter)
	server.(*authenticatorServer).svcCtx.PasswordLimiter = limiter

	req := new(authenticatorv1.ConfirmPasswordResetRequest)
	req.SetToken("missing-token")
	req.SetNewPassword("new-password")
	_, err := server.ConfirmPasswordReset(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidPasswordResetToken))

	req.SetToken("expired-token")
	_, err = server.ConfirmPasswordReset(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	calls, releases := limiter.snapshot()
	require.Zero(t, calls)
	require.Zero(t, releases)

	req.SetToken("")
	_, err = server.ConfirmPasswordReset(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	req.SetToken("missing-token")
	req.SetNewPassword("")
	_, err = server.ConfirmPasswordReset(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestRequestEmailVerificationStoresTokenAndSendsMail(t *testing.T) {
	sessionStore := newFakeSessionStore()
	mailerClient := new(fakeMailerClient)
	server := newRecoveryTestServer(t, sessionStore, &fakeUserClient{
		getUserResponse: recoveryTestUser(1001, "user@example.com", 0),
	}, mailerClient)

	req := new(authenticatorv1.RequestEmailVerificationRequest)
	req.SetUserId(1001)
	resp, err := server.RequestEmailVerification(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())

	delivered := mailerClient.onlyMail(t)
	require.Equal(t, mail.TemplateEmailVerification, delivered.template)
	stored := sessionStore.emailVerifications[token.Hash(delivered.token)]
	require.NotNil(t, stored)
	require.Equal(t, "user@example.com", stored.Email)
}

func TestRequestEmailVerificationAlreadyVerifiedSkipsMail(t *testing.T) {
	sessionStore := newFakeSessionStore()
	mailerClient := new(fakeMailerClient)
	server := newRecoveryTestServer(t, sessionStore, &fakeUserClient{
		getUserResponse: recoveryTestUser(1001, "user@example.com", 4001),
	}, mailerClient)

	req := new(authenticatorv1.RequestEmailVerificationRequest)
	req.SetUserId(1001)
	resp, err := server.RequestEmailVerification(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Empty(t, mailerClient.sent)
	require.Empty(t, sessionStore.emailVerifications)
}

func TestConfirmEmailVerificationSuccess(t *testing.T) {
	sessionStore := newFakeSessionStore()
	now := time.Now().UnixMilli()
	sessionStore.emailVerifications[token.Hash("raw-verify-token")] = &model.EmailVerificationToken{
		UserID:    1001,
		TokenHash: token.Hash("raw-verify-token"),
		Email:     "user@example.com",
		CreatedAt: now,
		ExpiresAt: now + 60_000,
	}
	userClient := &fakeUserClient{}
	server := newRecoveryTestServer(t, sessionStore, userClient, new(fakeMailerClient))

	req := new(authenticatorv1.ConfirmEmailVerificationRequest)
	req.SetToken("raw-verify-token")
	resp, err := server.ConfirmEmailVerification(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())

	require.Equal(t, int64(1001), userClient.markEmailVerifiedRequest.GetUserId())
	require.Equal(t, "user@example.com", userClient.markEmailVerifiedRequest.GetEmail())
	require.NotZero(t, userClient.markEmailVerifiedRequest.GetVerifiedAt())
	require.NotZero(t, sessionStore.emailVerifications[token.Hash("raw-verify-token")].ConsumedAt)

	_, err = server.ConfirmEmailVerification(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidEmailVerificationToken))
}

func TestConfirmEmailVerificationStaleEmail(t *testing.T) {
	sessionStore := newFakeSessionStore()
	now := time.Now().UnixMilli()
	sessionStore.emailVerifications[token.Hash("stale-token")] = &model.EmailVerificationToken{
		UserID:    1001,
		TokenHash: token.Hash("stale-token"),
		Email:     "old@example.com",
		CreatedAt: now,
		ExpiresAt: now + 60_000,
	}
	server := newRecoveryTestServer(t, sessionStore, &fakeUserClient{
		markEmailVerifiedErr: status.Error(codes.NotFound, "resource not found"),
	}, new(fakeMailerClient))

	req := new(authenticatorv1.ConfirmEmailVerificationRequest)
	req.SetToken("stale-token")
	_, err := server.ConfirmEmailVerification(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidEmailVerificationToken))
}

type fakeRecoveryLimiter struct {
	allow bool
	err   error
	keys  []string
}

func (l *fakeRecoveryLimiter) Allow(_ context.Context, key string) (bool, error) {
	l.keys = append(l.keys, key)
	if l.err != nil {
		return false, l.err
	}
	return l.allow, nil
}

func TestRequestPasswordResetThrottledStaysSilent(t *testing.T) {
	sessionStore := newFakeSessionStore()
	mailerClient := new(fakeMailerClient)
	limiter := &fakeRecoveryLimiter{allow: false}
	server := newThrottledRecoveryTestServer(t, sessionStore, &fakeUserClient{
		getUserResponse: recoveryTestUser(1001, "user@example.com", 0),
	}, mailerClient, limiter)

	req := new(authenticatorv1.RequestPasswordResetRequest)
	req.SetEmail("User@Example.com")
	resp, err := server.RequestPasswordReset(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Empty(t, sessionStore.passwordResets)
	require.Empty(t, mailerClient.sent)

	// The throttle key is derived from the lowercased email hash.
	require.Len(t, limiter.keys, 1)
	require.Equal(t, "pwreset:"+token.Hash("user@example.com"), limiter.keys[0])
}

func TestRequestEmailVerificationThrottledStaysSilent(t *testing.T) {
	sessionStore := newFakeSessionStore()
	mailerClient := new(fakeMailerClient)
	limiter := &fakeRecoveryLimiter{allow: false}
	server := newThrottledRecoveryTestServer(t, sessionStore, &fakeUserClient{
		getUserResponse: recoveryTestUser(1001, "user@example.com", 0),
	}, mailerClient, limiter)

	req := new(authenticatorv1.RequestEmailVerificationRequest)
	req.SetUserId(1001)
	resp, err := server.RequestEmailVerification(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Empty(t, sessionStore.emailVerifications)
	require.Empty(t, mailerClient.sent)
	require.Equal(t, []string{"emailverify:1001"}, limiter.keys)
}

func TestRecoveryLimiterFailsOpen(t *testing.T) {
	sessionStore := newFakeSessionStore()
	seedCredential(t, sessionStore, 1001, "old-password")
	mailerClient := new(fakeMailerClient)
	limiter := &fakeRecoveryLimiter{err: errors.New("redis down")}
	server := newThrottledRecoveryTestServer(t, sessionStore, &fakeUserClient{
		getUserResponse: recoveryTestUser(1001, "user@example.com", 0),
	}, mailerClient, limiter)

	req := new(authenticatorv1.RequestPasswordResetRequest)
	req.SetEmail("user@example.com")
	resp, err := server.RequestPasswordReset(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Len(t, sessionStore.passwordResets, 1)
	require.Len(t, mailerClient.sent, 1)
}
