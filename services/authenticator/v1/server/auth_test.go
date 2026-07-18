package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/password"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/authenticator/v1/config"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/store"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/token"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/twofactor"
	"github.com/soasurs/cordis/services/authenticator/v1/svc"
)

func TestRegister(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	userClient := &fakeUserClient{
		createUserResponse: createUserResponse(1001, "user@example.com"),
	}
	server := newTestAuthenticatorServer(t, store, tokens, userClient)
	limiter := new(recordingPasswordLimiter)
	server.(*authenticatorServer).svcCtx.PasswordLimiter = limiter

	req := new(authenticatorv1.RegisterRequest)
	req.SetName("  display name  ")
	req.SetUsername("tester")
	req.SetEmail("user@example.com")
	req.SetPassword("password")
	req.SetUserAgent("test-agent")
	req.SetIp("127.0.0.1")

	resp, err := server.Register(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "display name", userClient.createUserRequest.GetName())
	require.Equal(t, "user@example.com", userClient.createUserRequest.GetEmail())
	require.NotNil(t, store.credentials[1001])
	match, err := password.Verify(store.credentials[1001].HashedPassword, "password")
	require.NoError(t, err)
	require.True(t, match)
	result := resp.GetResult()
	require.True(t, result.GetOk())
	require.Equal(t, int64(1001), result.GetUserId())
	require.NotEmpty(t, result.GetAccessToken())
	require.NotEmpty(t, result.GetRefreshToken())
	require.NotNil(t, store.createdSession)
	require.Equal(t, "test-agent", store.createdSession.UserAgent)
	require.Equal(t, "127.0.0.1", store.createdSession.IP)
	calls, releases := limiter.snapshot()
	require.Equal(t, 1, calls)
	require.Equal(t, 1, releases)
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

func TestLogin(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	seedCredential(t, store, 1001, "password")
	server := newTestAuthenticatorServer(t, store, tokens, &fakeUserClient{
		getUserResponse: userResponse(1001, "user@example.com"),
	})
	limiter := new(recordingPasswordLimiter)
	server.(*authenticatorServer).svcCtx.PasswordLimiter = limiter

	req := new(authenticatorv1.LoginRequest)
	req.SetEmail("user@example.com")
	req.SetPassword("password")
	req.SetUserAgent("test-agent")
	req.SetIp("127.0.0.1")

	resp, err := server.Login(context.Background(), req)
	require.NoError(t, err)
	result := resp.GetResult()
	require.True(t, result.GetOk())
	require.Equal(t, int64(1001), result.GetUserId())
	require.NotZero(t, result.GetSessionId())
	require.NotEmpty(t, result.GetAccessToken())
	require.NotEmpty(t, result.GetRefreshToken())
	require.NotNil(t, store.createdSession)
	require.Equal(t, "test-agent", store.createdSession.UserAgent)
	require.Equal(t, "127.0.0.1", store.createdSession.IP)
	calls, releases := limiter.snapshot()
	require.Equal(t, 1, calls)
	require.Equal(t, 1, releases)
}

func TestLoginUnknownEmailUsesPasswordLimiter(t *testing.T) {
	server := newTestAuthenticatorServer(t, newFakeSessionStore(), newTestTokenManager(t), &fakeUserClient{
		getUserErr: status.Error(codes.NotFound, "user not found"),
	})
	limiter := new(recordingPasswordLimiter)
	server.(*authenticatorServer).svcCtx.PasswordLimiter = limiter

	req := new(authenticatorv1.LoginRequest)
	req.SetEmail("missing@example.com")
	req.SetPassword("password")
	_, err := server.Login(t.Context(), req)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	calls, releases := limiter.snapshot()
	require.Equal(t, 1, calls)
	require.Equal(t, 1, releases)
}

func TestLoginInvalidCredentials(t *testing.T) {
	store := newFakeSessionStore()
	seedCredential(t, store, 1001, "password")
	server := newTestAuthenticatorServer(t, store, newTestTokenManager(t), &fakeUserClient{
		getUserResponse: userResponse(1001, "user@example.com"),
	})

	req := new(authenticatorv1.LoginRequest)
	req.SetEmail("user@example.com")
	req.SetPassword("wrong-password")

	_, err := server.Login(context.Background(), req)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidCredentials))
}

func TestLoginRequiresAndCompletesTwoFactor(t *testing.T) {
	store := newFakeSessionStore()
	seedCredential(t, store, 1001, "password")
	server := newTestAuthenticatorServer(t, store, newTestTokenManager(t), &fakeUserClient{
		getUserResponse: userResponse(1001, "user@example.com"),
	})
	secret := []byte("12345678901234567890")
	ciphertext, err := server.(*authenticatorServer).svcCtx.TwoFactor.Encrypt(1001, secret)
	require.NoError(t, err)
	store.factors[1001] = &model.TOTPFactor{UserID: 1001, SecretCiphertext: ciphertext.Data, EncryptionKeyID: ciphertext.KeyID, LastUsedCounter: -1}

	loginReq := new(authenticatorv1.LoginRequest)
	loginReq.SetEmail("user@example.com")
	loginReq.SetPassword("password")
	loginReq.SetUserAgent("test-agent")
	loginReq.SetIp("127.0.0.1")
	loginResp, err := server.Login(context.Background(), loginReq)
	require.NoError(t, err)
	require.Nil(t, loginResp.GetResult())
	require.NotEmpty(t, loginResp.GetTwoFactorChallenge().GetToken())
	require.Nil(t, store.createdSession)

	completeReq := new(authenticatorv1.CompleteTwoFactorLoginRequest)
	completeReq.SetChallengeToken(loginResp.GetTwoFactorChallenge().GetToken())
	completeReq.SetCode(testTOTPCode(secret, time.Now()))
	completeResp, err := server.CompleteTwoFactorLogin(context.Background(), completeReq)
	require.NoError(t, err)
	require.NotEmpty(t, completeResp.GetResult().GetAccessToken())
	require.NotNil(t, store.createdSession)
	require.Equal(t, "test-agent", store.createdSession.UserAgent)
}

func TestTwoFactorEnrollmentCreatesRecoveryCodes(t *testing.T) {
	store := newFakeSessionStore()
	seedCredential(t, store, 1001, "password")
	server := newTestAuthenticatorServer(t, store, newTestTokenManager(t), &fakeUserClient{
		getUserResponse: userResponse(1001, "user@example.com"),
	})
	store.sessions[2001] = &model.Session{SessionID: 2001, UserID: 1001}

	beginReq := new(authenticatorv1.BeginTwoFactorEnrollmentRequest)
	beginReq.SetUserId(1001)
	beginReq.SetPassword("password")
	beginResp, err := server.BeginTwoFactorEnrollment(context.Background(), beginReq)
	require.NoError(t, err)
	require.NotEmpty(t, beginResp.GetOtpauthUri())
	require.NotEmpty(t, beginResp.GetManualEntryKey())
	require.NotEmpty(t, beginResp.GetEnrollmentToken())

	enrollment := store.enrollments[1001]
	secret, err := server.(*authenticatorServer).svcCtx.TwoFactor.Decrypt(1001, twofactor.Ciphertext{KeyID: enrollment.EncryptionKeyID, Data: enrollment.SecretCiphertext})
	require.NoError(t, err)
	confirmReq := new(authenticatorv1.ConfirmTwoFactorEnrollmentRequest)
	confirmReq.SetUserId(1001)
	confirmReq.SetCurrentSessionId(2001)
	confirmReq.SetEnrollmentToken(beginResp.GetEnrollmentToken())
	confirmReq.SetCode(testTOTPCode(secret, time.Now()))
	confirmResp, err := server.ConfirmTwoFactorEnrollment(context.Background(), confirmReq)
	require.NoError(t, err)
	require.Len(t, confirmResp.GetRecoveryCodes(), 10)
	require.NotNil(t, store.factors[1001])
	require.NotContains(t, store.enrollments, int64(1001))

	statusReq := new(authenticatorv1.GetTwoFactorStatusRequest)
	statusReq.SetUserId(1001)
	statusResp, err := server.GetTwoFactorStatus(context.Background(), statusReq)
	require.NoError(t, err)
	require.True(t, statusResp.GetEnabled())
	require.Equal(t, int32(10), statusResp.GetRecoveryCodesRemaining())
}

func TestBeginTwoFactorEnrollmentRejectsPendingEnrollment(t *testing.T) {
	store := newFakeSessionStore()
	seedCredential(t, store, 1001, "password")
	server := newTestAuthenticatorServer(t, store, newTestTokenManager(t), &fakeUserClient{
		getUserResponse: userResponse(1001, "user@example.com"),
	})
	req := new(authenticatorv1.BeginTwoFactorEnrollmentRequest)
	req.SetUserId(1001)
	req.SetPassword("password")

	_, err := server.BeginTwoFactorEnrollment(context.Background(), req)
	require.NoError(t, err)
	_, err = server.BeginTwoFactorEnrollment(context.Background(), req)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorTwoFactorEnrollmentPending))
}

func TestCompleteTwoFactorLoginInvalidCodeCountsAttempt(t *testing.T) {
	store := newFakeSessionStore()
	server := newTestAuthenticatorServer(t, store, newTestTokenManager(t), new(fakeUserClient))
	concrete := server.(*authenticatorServer)
	secret := []byte("12345678901234567890")
	ciphertext, err := concrete.svcCtx.TwoFactor.Encrypt(1001, secret)
	require.NoError(t, err)
	store.factors[1001] = &model.TOTPFactor{UserID: 1001, SecretCiphertext: ciphertext.Data, EncryptionKeyID: ciphertext.KeyID, LastUsedCounter: -1}
	challenge, err := concrete.createTwoFactorLoginChallenge(context.Background(), 1001, "agent", "127.0.0.1")
	require.NoError(t, err)

	req := new(authenticatorv1.CompleteTwoFactorLoginRequest)
	req.SetChallengeToken(challenge.GetToken())
	req.SetCode("abcdef")
	_, err = server.CompleteTwoFactorLogin(context.Background(), req)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidTwoFactorCode))
	require.Equal(t, 1, store.challenges[token.Hash(challenge.GetToken())].Attempts)
}

func TestCompleteTwoFactorLoginRejectsExpiredChallenge(t *testing.T) {
	store := newFakeSessionStore()
	server := newTestAuthenticatorServer(t, store, newTestTokenManager(t), new(fakeUserClient))
	concrete := server.(*authenticatorServer)
	challenge, err := concrete.createTwoFactorLoginChallenge(context.Background(), 1001, "agent", "127.0.0.1")
	require.NoError(t, err)
	store.challenges[token.Hash(challenge.GetToken())].ExpiresAt = time.Now().Add(-time.Minute).UnixMilli()

	req := new(authenticatorv1.CompleteTwoFactorLoginRequest)
	req.SetChallengeToken(challenge.GetToken())
	req.SetCode("abcdef")
	_, err = server.CompleteTwoFactorLogin(context.Background(), req)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorTwoFactorChallengeExpired))
}

func TestCompleteTwoFactorLoginAcceptsRecoveryCode(t *testing.T) {
	store := newFakeSessionStore()
	server := newTestAuthenticatorServer(t, store, newTestTokenManager(t), new(fakeUserClient))
	concrete := server.(*authenticatorServer)
	secret := []byte("12345678901234567890")
	ciphertext, err := concrete.svcCtx.TwoFactor.Encrypt(1001, secret)
	require.NoError(t, err)
	store.factors[1001] = &model.TOTPFactor{UserID: 1001, SecretCiphertext: ciphertext.Data, EncryptionKeyID: ciphertext.KeyID, LastUsedCounter: -1}
	recoveryCode := "ABCDE-FGHIJ-KLMNO-PQRST-UV"
	recoveryCodeHash := token.Hash(twofactor.NormalizeRecoveryCode(recoveryCode))
	store.recoveryCodes[1001] = map[string]int64{recoveryCodeHash: 0}
	challenge, err := concrete.createTwoFactorLoginChallenge(context.Background(), 1001, "agent", "127.0.0.1")
	require.NoError(t, err)

	req := new(authenticatorv1.CompleteTwoFactorLoginRequest)
	req.SetChallengeToken(challenge.GetToken())
	req.SetCode(recoveryCode)
	resp, err := server.CompleteTwoFactorLogin(context.Background(), req)
	require.NoError(t, err)
	require.NotEmpty(t, resp.GetResult().GetAccessToken())
	require.NotZero(t, store.recoveryCodes[1001][recoveryCodeHash])
}

func TestRefresh(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	session := createRefreshSession(t, store, tokens, 1001, 2001, time.Now().Add(time.Hour).UnixMilli())
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient))

	req := new(authenticatorv1.RefreshRequest)
	req.SetRefreshToken(session.refreshToken.Raw)

	resp, err := server.Refresh(context.Background(), req)
	require.NoError(t, err)
	result := resp.GetResult()
	require.True(t, result.GetOk())
	require.Equal(t, int64(1001), result.GetUserId())
	require.Equal(t, int64(2001), result.GetSessionId())
	require.NotEqual(t, session.refreshToken.Raw, result.GetRefreshToken())
	require.Equal(t, token.Hash(session.refreshToken.Raw), store.rotatedOldHash)
	require.Equal(t, token.Hash(result.GetRefreshToken()), store.rotatedNewHash)
}

func TestRefreshInvalidToken(t *testing.T) {
	server := newTestAuthenticatorServer(t, newFakeSessionStore(), newTestTokenManager(t), new(fakeUserClient))

	req := new(authenticatorv1.RefreshRequest)
	req.SetRefreshToken("invalid-token")

	_, err := server.Refresh(context.Background(), req)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidRefreshToken))
}

func TestRefreshHashMismatch(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	session := createRefreshSession(t, store, tokens, 1001, 2001, time.Now().Add(time.Hour).UnixMilli())
	session.session.RefreshTokenHash = token.Hash("other-token")
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient))

	req := new(authenticatorv1.RefreshRequest)
	req.SetRefreshToken(session.refreshToken.Raw)

	_, err := server.Refresh(context.Background(), req)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidRefreshToken))
}

func TestRefreshExpiredSession(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	session := createRefreshSession(t, store, tokens, 1001, 2001, time.Now().Add(time.Hour).UnixMilli())
	session.session.ExpiresAt = time.Now().Add(-time.Hour).UnixMilli()
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient))

	req := new(authenticatorv1.RefreshRequest)
	req.SetRefreshToken(session.refreshToken.Raw)

	_, err := server.Refresh(context.Background(), req)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorSessionExpired))
}

func TestRefreshRevokedSession(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	session := createRefreshSession(t, store, tokens, 1001, 2001, time.Now().Add(time.Hour).UnixMilli())
	session.session.RevokedAt = time.Now().UnixMilli()
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient))

	req := new(authenticatorv1.RefreshRequest)
	req.SetRefreshToken(session.refreshToken.Raw)

	_, err := server.Refresh(context.Background(), req)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorSessionRevoked))
}

func TestLogout(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	session := createRefreshSession(t, store, tokens, 1001, 2001, time.Now().Add(time.Hour).UnixMilli())
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient))

	req := new(authenticatorv1.LogoutRequest)
	req.SetRefreshToken(session.refreshToken.Raw)

	resp, err := server.Logout(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Equal(t, int64(2001), store.revokedSessionID)
}

func TestLogoutInvalidToken(t *testing.T) {
	server := newTestAuthenticatorServer(t, newFakeSessionStore(), newTestTokenManager(t), new(fakeUserClient))

	req := new(authenticatorv1.LogoutRequest)
	req.SetRefreshToken("invalid-token")

	_, err := server.Logout(context.Background(), req)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidRefreshToken))
}

func TestVerifyAccessToken(t *testing.T) {
	store := newFakeSessionStore()
	tokens := newTestTokenManager(t)
	sessionExpiresAt := time.Now().Add(time.Hour).UnixMilli()
	accessToken, err := tokens.IssueAccessToken(1001, 2001, time.Now())
	require.NoError(t, err)
	store.sessions[2001] = &model.Session{
		SessionID: 2001,
		UserID:    1001,
		ExpiresAt: sessionExpiresAt,
	}
	server := newTestAuthenticatorServer(t, store, tokens, new(fakeUserClient))

	req := new(authenticatorv1.VerifyAccessTokenRequest)
	req.SetAccessToken(accessToken.Raw)

	resp, err := server.VerifyAccessToken(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Equal(t, int64(1001), resp.GetUserId())
	require.Equal(t, int64(2001), resp.GetSessionId())
}

func TestVerifyAccessTokenInvalidToken(t *testing.T) {
	server := newTestAuthenticatorServer(t, newFakeSessionStore(), newTestTokenManager(t), new(fakeUserClient))

	req := new(authenticatorv1.VerifyAccessTokenRequest)
	req.SetAccessToken("invalid-token")

	_, err := server.VerifyAccessToken(context.Background(), req)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidAccessToken))
}

func TestListSessions(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions[2001] = &model.Session{
		SessionID: 2001,
		UserID:    1001,
		UserAgent: "agent",
		ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	}
	server := newTestAuthenticatorServer(t, store, newTestTokenManager(t), new(fakeUserClient))

	req := new(authenticatorv1.ListSessionsRequest)
	req.SetUserId(1001)
	resp, err := server.ListSessions(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetSessions(), 1)
	require.Equal(t, int64(2001), resp.GetSessions()[0].GetSessionId())
}

func TestRevokeUserSessionChecksOwner(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions[2001] = &model.Session{
		SessionID: 2001,
		UserID:    1002,
		ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	}
	server := newTestAuthenticatorServer(t, store, newTestTokenManager(t), new(fakeUserClient))

	req := new(authenticatorv1.RevokeUserSessionRequest)
	req.SetUserId(1001)
	req.SetSessionId(2001)
	_, err := server.RevokeUserSession(context.Background(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestRevokeOtherSessionsKeepsCurrent(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions[2001] = &model.Session{SessionID: 2001, UserID: 1001}
	store.sessions[2002] = &model.Session{SessionID: 2002, UserID: 1001}
	store.sessions[2003] = &model.Session{SessionID: 2003, UserID: 1002}
	server := newTestAuthenticatorServer(t, store, newTestTokenManager(t), new(fakeUserClient))

	req := new(authenticatorv1.RevokeOtherSessionsRequest)
	req.SetUserId(1001)
	req.SetCurrentSessionId(2001)
	resp, err := server.RevokeOtherSessions(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int32(1), resp.GetRevoked())
	require.Zero(t, store.sessions[2001].RevokedAt)
	require.NotZero(t, store.sessions[2002].RevokedAt)
	require.Zero(t, store.sessions[2003].RevokedAt)
}

func newTestAuthenticatorServer(t *testing.T, store store.Store, tokens *token.Manager, userClient userv1.UserServiceClient) authenticatorv1.AuthenticatorServiceServer {
	t.Helper()

	node, err := snowflake.New()
	require.NoError(t, err)

	return New(&svc.ServiceContext{
		Cfg: config.Config{
			Sessions: config.SessionConfig{
				TTL: time.Hour,
			},
			TwoFactor: config.TwoFactorConfig{
				Issuer: "Cordis Test", EnrollmentTTL: 10 * time.Minute, LoginChallengeTTL: 5 * time.Minute,
				MaxAttempts: 5, RecoveryCodeCount: 10,
			},
			Recovery: config.RecoveryConfig{
				PasswordResetTTL:     30 * time.Minute,
				EmailVerificationTTL: 24 * time.Hour,
			},
		},
		Store:      store,
		Tokens:     tokens,
		TwoFactor:  newTestTwoFactorCipher(t),
		Snowflake:  node,
		UserClient: userClient,
	})
}

func newTestTwoFactorCipher(t *testing.T) *twofactor.Cipher {
	t.Helper()
	cipher, err := twofactor.NewCipher("test", []twofactor.KeyConfig{{
		ID: "test", Secret: base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901")),
	}})
	require.NoError(t, err)
	return cipher
}

func newTestTokenManager(t *testing.T) *token.Manager {
	t.Helper()

	manager, err := token.NewManager(token.Config{
		Issuer:        "cordis.test",
		AccessSecret:  "test-access-secret-32-bytes-long",
		RefreshSecret: "test-refresh-secret-32-bytes-long",
		AccessTTL:     15 * time.Minute,
		RefreshTTL:    time.Hour,
	})
	require.NoError(t, err)
	return manager
}

func createUserResponse(userID int64, email string) *userv1.CreateUserResponse {
	user := new(userv1.User)
	user.SetUserId(userID)
	user.SetEmail(email)

	resp := new(userv1.CreateUserResponse)
	resp.SetUser(user)
	return resp
}

func userResponse(userID int64, email string) *userv1.GetUserResponse {
	user := new(userv1.User)
	user.SetUserId(userID)
	user.SetEmail(email)
	resp := new(userv1.GetUserResponse)
	resp.SetUser(user)
	return resp
}

func testTOTPCode(secret []byte, now time.Time) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(now.Unix()/30))
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(buf[:])
	digest := mac.Sum(nil)
	offset := int(digest[len(digest)-1] & 0x0f)
	value := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1_000_000)
}

type fakeUserClient struct {
	userv1.UserServiceClient
	createUserRequest  *userv1.CreateUserRequest
	createUserResponse *userv1.CreateUserResponse
	createUserErr      error
	getUserResponse    *userv1.GetUserResponse
	getUserErr         error

	markEmailVerifiedRequest *userv1.MarkEmailVerifiedRequest
	markEmailVerifiedErr     error
}

func (c *fakeUserClient) MarkEmailVerified(_ context.Context, req *userv1.MarkEmailVerifiedRequest, _ ...grpc.CallOption) (*userv1.MarkEmailVerifiedResponse, error) {
	c.markEmailVerifiedRequest = req
	if c.markEmailVerifiedErr != nil {
		return nil, c.markEmailVerifiedErr
	}
	resp := new(userv1.MarkEmailVerifiedResponse)
	resp.SetOk(true)
	return resp, nil
}

func (c *fakeUserClient) GetUser(_ context.Context, _ *userv1.GetUserRequest, _ ...grpc.CallOption) (*userv1.GetUserResponse, error) {
	if c.getUserErr != nil {
		return nil, c.getUserErr
	}
	return c.getUserResponse, nil
}

func (c *fakeUserClient) CreateUser(_ context.Context, req *userv1.CreateUserRequest, _ ...grpc.CallOption) (*userv1.CreateUserResponse, error) {
	c.createUserRequest = req
	if c.createUserErr != nil {
		return nil, c.createUserErr
	}
	return c.createUserResponse, nil
}

type fakeSessionStore struct {
	sessions           map[int64]*model.Session
	createdSession     *model.Session
	rotatedOldHash     string
	rotatedNewHash     string
	revokedSessionID   int64
	revokedOtherUserID int64
	currentSessionID   int64
	factors            map[int64]*model.TOTPFactor
	enrollments        map[int64]*model.TOTPEnrollment
	challenges         map[string]*model.TwoFactorLoginChallenge
	recoveryCodes      map[int64]map[string]int64
	passwordResets     map[string]*model.PasswordResetToken
	passwordResetReads []bool
	emailVerifications map[string]*model.EmailVerificationToken
	credentials        map[int64]*model.UserCredential
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{
		sessions:           make(map[int64]*model.Session),
		factors:            make(map[int64]*model.TOTPFactor),
		enrollments:        make(map[int64]*model.TOTPEnrollment),
		challenges:         make(map[string]*model.TwoFactorLoginChallenge),
		recoveryCodes:      make(map[int64]map[string]int64),
		passwordResets:     make(map[string]*model.PasswordResetToken),
		emailVerifications: make(map[string]*model.EmailVerificationToken),
		credentials:        make(map[int64]*model.UserCredential),
	}
}

func (s *fakeSessionStore) Transact(_ context.Context, fn func(store.Store) error) error {
	return fn(s)
}

func (s *fakeSessionStore) CreateSession(_ context.Context, sessionID, userID int64, refreshTokenHash, userAgent, ip string, expiresAt int64) (*model.Session, error) {
	session := &model.Session{
		SessionID:        sessionID,
		UserID:           userID,
		RefreshTokenHash: refreshTokenHash,
		UserAgent:        userAgent,
		IP:               ip,
		ExpiresAt:        expiresAt,
	}
	s.createdSession = session
	s.sessions[sessionID] = session
	return session, nil
}

func (s *fakeSessionStore) GetSession(_ context.Context, sessionID int64) (*model.Session, error) {
	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return session, nil
}

func (s *fakeSessionStore) ListSessions(_ context.Context, userID int64) ([]*model.Session, error) {
	sessions := make([]*model.Session, 0)
	for _, session := range s.sessions {
		if session.UserID == userID && session.RevokedAt == 0 && session.ExpiresAt > time.Now().UnixMilli() {
			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}

func (s *fakeSessionStore) RotateRefreshToken(_ context.Context, sessionID int64, oldRefreshTokenHash, newRefreshTokenHash string) error {
	session, ok := s.sessions[sessionID]
	if !ok || session.RefreshTokenHash != oldRefreshTokenHash {
		return sql.ErrNoRows
	}
	s.rotatedOldHash = oldRefreshTokenHash
	s.rotatedNewHash = newRefreshTokenHash
	session.RefreshTokenHash = newRefreshTokenHash
	return nil
}

func (s *fakeSessionStore) RevokeSession(_ context.Context, sessionID int64) error {
	session, ok := s.sessions[sessionID]
	if !ok {
		return sql.ErrNoRows
	}
	s.revokedSessionID = sessionID
	session.RevokedAt = time.Now().UnixMilli()
	return nil
}

func (s *fakeSessionStore) RevokeUserSession(_ context.Context, userID, sessionID int64) error {
	session, ok := s.sessions[sessionID]
	if !ok || session.UserID != userID || session.RevokedAt != 0 {
		return sql.ErrNoRows
	}
	s.revokedSessionID = sessionID
	session.RevokedAt = time.Now().UnixMilli()
	return nil
}

func (s *fakeSessionStore) RevokeOtherSessions(_ context.Context, userID, currentSessionID int64) (int64, error) {
	s.revokedOtherUserID = userID
	s.currentSessionID = currentSessionID
	var revoked int64
	for _, session := range s.sessions {
		if session.UserID == userID && session.SessionID != currentSessionID && session.RevokedAt == 0 {
			session.RevokedAt = time.Now().UnixMilli()
			revoked++
		}
	}
	return revoked, nil
}

func (s *fakeSessionStore) GetTOTPFactor(_ context.Context, userID int64, _ bool) (*model.TOTPFactor, error) {
	factor, ok := s.factors[userID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return factor, nil
}

func (s *fakeSessionStore) CreateTOTPEnrollment(_ context.Context, enrollment *model.TOTPEnrollment) error {
	if existing, ok := s.enrollments[enrollment.UserID]; ok && existing.ExpiresAt > enrollment.CreatedAt {
		return sql.ErrNoRows
	}
	s.enrollments[enrollment.UserID] = enrollment
	return nil
}

func (s *fakeSessionStore) GetTOTPEnrollment(_ context.Context, userID int64, tokenHash string, _ bool) (*model.TOTPEnrollment, error) {
	enrollment, ok := s.enrollments[userID]
	if !ok || enrollment.TokenHash != tokenHash {
		return nil, sql.ErrNoRows
	}
	return enrollment, nil
}

func (s *fakeSessionStore) DeleteTOTPEnrollment(_ context.Context, userID int64, tokenHash string) error {
	enrollment, ok := s.enrollments[userID]
	if !ok || enrollment.TokenHash != tokenHash {
		return sql.ErrNoRows
	}
	delete(s.enrollments, userID)
	return nil
}

func (s *fakeSessionStore) UpsertTOTPFactor(_ context.Context, factor *model.TOTPFactor) error {
	s.factors[factor.UserID] = factor
	return nil
}

func (s *fakeSessionStore) DeleteTOTPFactor(_ context.Context, userID int64) error {
	if _, ok := s.factors[userID]; !ok {
		return sql.ErrNoRows
	}
	delete(s.factors, userID)
	return nil
}

func (s *fakeSessionStore) UpdateTOTPLastUsedCounter(_ context.Context, userID, counter int64) error {
	factor, ok := s.factors[userID]
	if !ok || factor.LastUsedCounter >= counter {
		return sql.ErrNoRows
	}
	factor.LastUsedCounter = counter
	return nil
}

func (s *fakeSessionStore) CreateTwoFactorLoginChallenge(_ context.Context, challenge *model.TwoFactorLoginChallenge) error {
	s.challenges[challenge.TokenHash] = challenge
	return nil
}

func (s *fakeSessionStore) GetTwoFactorLoginChallenge(_ context.Context, tokenHash string, _ bool) (*model.TwoFactorLoginChallenge, error) {
	challenge, ok := s.challenges[tokenHash]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return challenge, nil
}

func (s *fakeSessionStore) IncrementTwoFactorLoginChallengeAttempts(_ context.Context, tokenHash string) error {
	challenge, ok := s.challenges[tokenHash]
	if !ok || challenge.ConsumedAt != 0 {
		return sql.ErrNoRows
	}
	challenge.Attempts++
	return nil
}

func (s *fakeSessionStore) ConsumeTwoFactorLoginChallenge(_ context.Context, tokenHash string) error {
	challenge, ok := s.challenges[tokenHash]
	if !ok || challenge.ConsumedAt != 0 {
		return sql.ErrNoRows
	}
	challenge.ConsumedAt = time.Now().UnixMilli()
	return nil
}

func (s *fakeSessionStore) ReplaceRecoveryCodes(_ context.Context, userID int64, codeHashes []string) error {
	codes := make(map[string]int64, len(codeHashes))
	for _, hash := range codeHashes {
		codes[hash] = 0
	}
	s.recoveryCodes[userID] = codes
	return nil
}

func (s *fakeSessionStore) CountUnusedRecoveryCodes(_ context.Context, userID int64) (int64, error) {
	var count int64
	for _, usedAt := range s.recoveryCodes[userID] {
		if usedAt == 0 {
			count++
		}
	}
	return count, nil
}

func (s *fakeSessionStore) ConsumeRecoveryCode(_ context.Context, userID int64, codeHash string) error {
	codes, ok := s.recoveryCodes[userID]
	if !ok || codes[codeHash] != 0 {
		return sql.ErrNoRows
	}
	codes[codeHash] = time.Now().UnixMilli()
	return nil
}

type refreshSession struct {
	session      *model.Session
	refreshToken token.Token
}

func createRefreshSession(t *testing.T, store *fakeSessionStore, tokens *token.Manager, userID, sessionID, sessionExpiresAt int64) refreshSession {
	t.Helper()

	refreshToken, err := tokens.IssueRefreshToken(userID, sessionID, sessionExpiresAt, time.Now())
	require.NoError(t, err)

	session := &model.Session{
		SessionID:        sessionID,
		UserID:           userID,
		RefreshTokenHash: token.Hash(refreshToken.Raw),
		ExpiresAt:        sessionExpiresAt,
	}
	store.sessions[sessionID] = session
	return refreshSession{
		session:      session,
		refreshToken: refreshToken,
	}
}

func (s *fakeSessionStore) UpsertPasswordResetToken(_ context.Context, token *model.PasswordResetToken) error {
	for hash, existing := range s.passwordResets {
		if existing.UserID == token.UserID {
			delete(s.passwordResets, hash)
		}
	}
	value := *token
	s.passwordResets[token.TokenHash] = &value
	return nil
}

func (s *fakeSessionStore) GetPasswordResetToken(_ context.Context, tokenHash string, forUpdate bool) (*model.PasswordResetToken, error) {
	s.passwordResetReads = append(s.passwordResetReads, forUpdate)
	token, ok := s.passwordResets[tokenHash]
	if !ok {
		return nil, sql.ErrNoRows
	}
	value := *token
	return &value, nil
}

func (s *fakeSessionStore) ConsumePasswordResetToken(_ context.Context, tokenHash string, consumedAt int64) error {
	token, ok := s.passwordResets[tokenHash]
	if !ok || token.ConsumedAt != 0 {
		return sql.ErrNoRows
	}
	token.ConsumedAt = consumedAt
	return nil
}

func (s *fakeSessionStore) UpsertEmailVerificationToken(_ context.Context, token *model.EmailVerificationToken) error {
	for hash, existing := range s.emailVerifications {
		if existing.UserID == token.UserID {
			delete(s.emailVerifications, hash)
		}
	}
	value := *token
	s.emailVerifications[token.TokenHash] = &value
	return nil
}

func (s *fakeSessionStore) GetEmailVerificationToken(_ context.Context, tokenHash string, _ bool) (*model.EmailVerificationToken, error) {
	token, ok := s.emailVerifications[tokenHash]
	if !ok {
		return nil, sql.ErrNoRows
	}
	value := *token
	return &value, nil
}

func (s *fakeSessionStore) ConsumeEmailVerificationToken(_ context.Context, tokenHash string, consumedAt int64) error {
	token, ok := s.emailVerifications[tokenHash]
	if !ok || token.ConsumedAt != 0 {
		return sql.ErrNoRows
	}
	token.ConsumedAt = consumedAt
	return nil
}

func (s *fakeSessionStore) CreateUserCredential(_ context.Context, credential *model.UserCredential) error {
	if _, ok := s.credentials[credential.UserID]; ok {
		return sql.ErrNoRows
	}
	value := *credential
	s.credentials[credential.UserID] = &value
	return nil
}

func (s *fakeSessionStore) GetUserCredential(_ context.Context, userID int64, _ bool) (*model.UserCredential, error) {
	credential, ok := s.credentials[userID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	value := *credential
	return &value, nil
}

func (s *fakeSessionStore) UpdateUserCredential(_ context.Context, userID int64, hashedPassword string, updatedAt int64) error {
	credential, ok := s.credentials[userID]
	if !ok {
		return sql.ErrNoRows
	}
	credential.HashedPassword = hashedPassword
	credential.UpdatedAt = updatedAt
	return nil
}

func (s *fakeSessionStore) UpsertUserCredential(_ context.Context, userID int64, hashedPassword string, now int64) error {
	if credential, ok := s.credentials[userID]; ok {
		credential.HashedPassword = hashedPassword
		credential.UpdatedAt = now
		return nil
	}
	s.credentials[userID] = &model.UserCredential{UserID: userID, HashedPassword: hashedPassword, CreatedAt: now}
	return nil
}

// seedCredential stores a hashed credential so password checks run against
// the authenticator-owned store.
func seedCredential(t *testing.T, store *fakeSessionStore, userID int64, plainPassword string) {
	t.Helper()
	hashed, err := password.Hash(plainPassword)
	require.NoError(t, err)
	store.credentials[userID] = &model.UserCredential{UserID: userID, HashedPassword: hashed, CreatedAt: 1}
}
