package server

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/password"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/authenticator/v1/config"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/store"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/token"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/twofactor"
	"github.com/soasurs/cordis/services/authenticator/v1/svc"
)

func newTestAuthenticatorServer(t *testing.T, store store.Store, tokens *token.Manager, userClient userv1.UserServiceClient) authenticatorv1.AuthenticatorServiceServer {
	t.Helper()

	node, err := snowflake.New()
	require.NoError(t, err)

	return New(&svc.ServiceContext{
		Cfg: config.Config{
			Sessions: config.SessionConfig{
				IdleTTL: time.Hour, AbsoluteTTL: 24 * time.Hour, RotationGrace: 30 * time.Second,
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
	user.SetEmailVerifiedAt(1)
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

type refreshSession struct {
	session      *model.Session
	refreshToken token.Token
}

func createRefreshSession(t *testing.T, store *fakeSessionStore, tokens *token.Manager, userID, sessionID, sessionExpiresAt int64) refreshSession {
	t.Helper()

	refreshToken, err := tokens.IssueRefreshToken(userID, sessionID, sessionExpiresAt, time.Now())
	require.NoError(t, err)

	session := &model.Session{
		SessionID:             sessionID,
		UserID:                userID,
		RefreshTokenHash:      token.Hash(refreshToken.Raw),
		RefreshTokenID:        refreshToken.ID,
		RefreshTokenIssuedAt:  refreshToken.IssuedAt,
		RefreshTokenExpiresAt: refreshToken.ExpiresAt,
		ExpiresAt:             sessionExpiresAt,
		AbsoluteExpiresAt:     sessionExpiresAt,
	}
	store.sessions[sessionID] = session
	return refreshSession{
		session:      session,
		refreshToken: refreshToken,
	}
}

func seedCredential(t *testing.T, store *fakeSessionStore, userID int64, plainPassword string) {
	t.Helper()
	hashed, err := password.Hash(plainPassword)
	require.NoError(t, err)
	store.credentials[userID] = &model.UserCredential{UserID: userID, HashedPassword: hashed, CreatedAt: 1}
}
