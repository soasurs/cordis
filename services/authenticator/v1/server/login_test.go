package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/token"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/twofactor"
)

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

func TestLoginUnverifiedEmailUsesInvalidCredentials(t *testing.T) {
	store := newFakeSessionStore()
	seedCredential(t, store, 1001, "password")
	user := userResponse(1001, "user@example.com")
	user.GetUser().SetEmailVerifiedAt(0)
	server := newTestAuthenticatorServer(t, store, newTestTokenManager(t), &fakeUserClient{
		getUserResponse: user,
	})

	req := new(authenticatorv1.LoginRequest)
	req.SetEmail("user@example.com")
	req.SetPassword("password")

	_, err := server.Login(t.Context(), req)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidCredentials))
	require.Nil(t, store.createdSession)
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
