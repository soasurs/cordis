//go:build integration

package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/mail"
	"github.com/soasurs/cordis/pkg/migration"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/authenticator/v1/config"
	authmigrations "github.com/soasurs/cordis/services/authenticator/v1/db/migrations"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/store"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/token"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/twofactor"
	"github.com/soasurs/cordis/services/authenticator/v1/svc"
	usermigrations "github.com/soasurs/cordis/services/user/v1/db/migrations"
)

// TestAuthenticatorUserComposition runs the Authenticator server in-process
// against a real User service binary so that registration and password
// verification cross real gRPC and Argon2id hashing instead of fakes.
func TestAuthenticatorUserComposition(t *testing.T) {
	compositionMailer := new(fakeMailerClient)
	postgres := testkit.StartPostgres(t)
	db, err := database.NewPostgres(database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, migration.Apply(t.Context(), db, usermigrations.Files))
	require.NoError(t, migration.Apply(t.Context(), db, authmigrations.Files))

	userAddress := startUserServiceForAuth(t, postgres.DSN)
	userConn, err := grpc.NewClient(userAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, userConn.Close()) })
	userClient := userv1.NewUserServiceClient(userConn)
	testkit.WaitServiceReady(t, 30*time.Second, func(ctx context.Context) error {
		req := new(userv1.CheckEmailAvailabilityRequest)
		req.SetEmail("probe@example.com")
		_, err := userClient.CheckEmailAvailability(ctx, req)
		return err
	})

	service := New(newCompositionServiceContext(t, db, userClient, compositionMailer))
	ctx := t.Context()

	var refreshToken string
	t.Run("register creates user and session", func(t *testing.T) {
		req := new(authenticatorv1.RegisterRequest)
		req.SetName("Alice")
		req.SetUsername("alice")
		req.SetEmail("alice@example.com")
		req.SetPassword("integration-password-1")
		resp, err := service.Register(ctx, req)
		require.NoError(t, err)
		result := resp.GetResult()
		require.True(t, result.GetOk())
		require.Positive(t, result.GetUserId())
		require.NotEmpty(t, result.GetAccessToken())
		require.NotEmpty(t, result.GetRefreshToken())
	})

	t.Run("duplicate email propagates already exists", func(t *testing.T) {
		req := new(authenticatorv1.RegisterRequest)
		req.SetName("Alice2")
		req.SetUsername("alice2")
		req.SetEmail("alice@example.com")
		req.SetPassword("integration-password-2")
		_, err := service.Register(ctx, req)
		require.Equal(t, codes.AlreadyExists, status.Code(err))
		require.True(t, rpcerror.Is(err, rpcerror.UserDomain, rpcerror.UserEmailAlreadyExists))
	})

	t.Run("login rejects wrong password", func(t *testing.T) {
		req := new(authenticatorv1.LoginRequest)
		req.SetEmail("alice@example.com")
		req.SetPassword("wrong-password")
		_, err := service.Login(ctx, req)
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("login verifies password through real user service", func(t *testing.T) {
		req := new(authenticatorv1.LoginRequest)
		req.SetEmail("alice@example.com")
		req.SetPassword("integration-password-1")
		resp, err := service.Login(ctx, req)
		require.NoError(t, err)
		result := resp.GetResult()
		require.True(t, result.GetOk())
		require.NotEmpty(t, result.GetRefreshToken())
		refreshToken = result.GetRefreshToken()
	})

	t.Run("refresh rotates the session token", func(t *testing.T) {
		req := new(authenticatorv1.RefreshRequest)
		req.SetRefreshToken(refreshToken)
		resp, err := service.Refresh(ctx, req)
		require.NoError(t, err)
		require.NotEmpty(t, resp.GetResult().GetRefreshToken())
		require.NotEqual(t, refreshToken, resp.GetResult().GetRefreshToken())

		req = new(authenticatorv1.RefreshRequest)
		req.SetRefreshToken(refreshToken)
		_, err = service.Refresh(ctx, req)
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("password reset replaces password and revokes sessions", func(t *testing.T) {
		loginReq := new(authenticatorv1.LoginRequest)
		loginReq.SetEmail("alice@example.com")
		loginReq.SetPassword("integration-password-1")
		loginResp, err := service.Login(ctx, loginReq)
		require.NoError(t, err)
		activeRefreshToken := loginResp.GetResult().GetRefreshToken()

		requestReq := new(authenticatorv1.RequestPasswordResetRequest)
		requestReq.SetEmail("alice@example.com")
		requestResp, err := service.RequestPasswordReset(ctx, requestReq)
		require.NoError(t, err)
		require.True(t, requestResp.GetOk())
		require.NotEmpty(t, compositionMailer.sent)
		delivered := compositionMailer.sent[len(compositionMailer.sent)-1]
		require.Equal(t, mail.TemplatePasswordReset, delivered.template)

		confirm := func() error {
			confirmReq := new(authenticatorv1.ConfirmPasswordResetRequest)
			confirmReq.SetToken(delivered.token)
			confirmReq.SetNewPassword("integration-password-3")
			_, err := service.ConfirmPasswordReset(ctx, confirmReq)
			return err
		}
		results := make(chan error, 2)
		for range 2 {
			go func() { results <- confirm() }()
		}
		var succeeded, rejected int
		for range 2 {
			err := <-results
			if err == nil {
				succeeded++
				continue
			}
			require.Equal(t, codes.InvalidArgument, status.Code(err))
			require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidPasswordResetToken))
			rejected++
		}
		require.Equal(t, 1, succeeded)
		require.Equal(t, 1, rejected)

		// The old password no longer works; the new one does.
		_, err = service.Login(ctx, loginReq)
		require.Equal(t, codes.Unauthenticated, status.Code(err))
		loginReq.SetPassword("integration-password-3")
		_, err = service.Login(ctx, loginReq)
		require.NoError(t, err)

		// Every pre-reset session is revoked.
		refreshReq := new(authenticatorv1.RefreshRequest)
		refreshReq.SetRefreshToken(activeRefreshToken)
		_, err = service.Refresh(ctx, refreshReq)
		require.Equal(t, codes.Unauthenticated, status.Code(err))

		// The reset token is single use.
		err = confirm()
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidPasswordResetToken))
	})

	t.Run("email verification marks user and rejects stale tokens", func(t *testing.T) {
		loginReq := new(authenticatorv1.LoginRequest)
		loginReq.SetEmail("alice@example.com")
		loginReq.SetPassword("integration-password-3")
		loginResp, err := service.Login(ctx, loginReq)
		require.NoError(t, err)
		aliceID := loginResp.GetResult().GetUserId()

		requestReq := new(authenticatorv1.RequestEmailVerificationRequest)
		requestReq.SetUserId(aliceID)
		_, err = service.RequestEmailVerification(ctx, requestReq)
		require.NoError(t, err)
		delivered := compositionMailer.sent[len(compositionMailer.sent)-1]
		require.Equal(t, mail.TemplateEmailVerification, delivered.template)

		confirmReq := new(authenticatorv1.ConfirmEmailVerificationRequest)
		confirmReq.SetToken(delivered.token)
		confirmResp, err := service.ConfirmEmailVerification(ctx, confirmReq)
		require.NoError(t, err)
		require.True(t, confirmResp.GetOk())

		getUserReq := new(userv1.GetUserRequest)
		getUserReq.SetUserId(aliceID)
		getUserResp, err := userClient.GetUser(ctx, getUserReq)
		require.NoError(t, err)
		require.NotZero(t, getUserResp.GetUser().GetEmailVerifiedAt())

		// A pending token issued before an email change must not verify the
		// replacement address.
		_, err = service.RequestEmailVerification(ctx, requestReq)
		require.NoError(t, err)
		require.Equal(t, mail.TemplateEmailVerification, compositionMailer.sent[len(compositionMailer.sent)-1].template)
		staleToken := compositionMailer.sent[len(compositionMailer.sent)-1].token

		updateEmailReq := new(userv1.UpdateEmailRequest)
		updateEmailReq.SetUserId(aliceID)
		updateEmailReq.SetEmail("alice-changed@example.com")
		updateEmailResp, err := userClient.UpdateEmail(ctx, updateEmailReq)
		require.NoError(t, err)
		require.Zero(t, updateEmailResp.GetUser().GetEmailVerifiedAt())

		staleConfirm := new(authenticatorv1.ConfirmEmailVerificationRequest)
		staleConfirm.SetToken(staleToken)
		_, err = service.ConfirmEmailVerification(ctx, staleConfirm)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.True(t, rpcerror.Is(err, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidEmailVerificationToken))

		getUserResp, err = userClient.GetUser(ctx, getUserReq)
		require.NoError(t, err)
		require.Zero(t, getUserResp.GetUser().GetEmailVerifiedAt())
	})

	t.Run("change password rotates credential and revokes other sessions", func(t *testing.T) {
		// The previous subtest moved alice to alice-changed@example.com.
		login := func(password string) *authenticatorv1.AuthenticationResult {
			loginReq := new(authenticatorv1.LoginRequest)
			loginReq.SetEmail("alice-changed@example.com")
			loginReq.SetPassword(password)
			loginResp, err := service.Login(ctx, loginReq)
			require.NoError(t, err)
			require.True(t, loginResp.GetResult().GetOk())
			return loginResp.GetResult()
		}
		current := login("integration-password-3")
		other := login("integration-password-3")

		// A wrong old password is a negative result and changes nothing.
		changeReq := new(authenticatorv1.ChangePasswordRequest)
		changeReq.SetUserId(current.GetUserId())
		changeReq.SetCurrentSessionId(current.GetSessionId())
		changeReq.SetOldPassword("wrong-password")
		changeReq.SetNewPassword("integration-password-4")
		changeResp, err := service.ChangePassword(ctx, changeReq)
		require.NoError(t, err)
		require.False(t, changeResp.GetOk())

		changeReq.SetOldPassword("integration-password-3")
		changeResp, err = service.ChangePassword(ctx, changeReq)
		require.NoError(t, err)
		require.True(t, changeResp.GetOk())

		// The old password stops working and the new one logs in.
		loginReq := new(authenticatorv1.LoginRequest)
		loginReq.SetEmail("alice-changed@example.com")
		loginReq.SetPassword("integration-password-3")
		_, err = service.Login(ctx, loginReq)
		require.Equal(t, codes.Unauthenticated, status.Code(err))
		login("integration-password-4")

		// The other session is revoked while the current one survives.
		refreshReq := new(authenticatorv1.RefreshRequest)
		refreshReq.SetRefreshToken(other.GetRefreshToken())
		_, err = service.Refresh(ctx, refreshReq)
		require.Equal(t, codes.Unauthenticated, status.Code(err))

		refreshReq = new(authenticatorv1.RefreshRequest)
		refreshReq.SetRefreshToken(current.GetRefreshToken())
		refreshResp, err := service.Refresh(ctx, refreshReq)
		require.NoError(t, err)
		require.True(t, refreshResp.GetResult().GetOk())
	})
}

func startUserServiceForAuth(t *testing.T, dsn string) string {
	t.Helper()
	address := testkit.FreeAddress(t)
	binary := testkit.BuildService(t, "github.com/soasurs/cordis/services/user/v1")
	testkit.StartService(t, binary, fmt.Sprintf(`
name: user.v1
listenOn: %s
timeout: 0
log:
  level: error
  stat: false
database:
  dataSource: %s
services:
  media:
    endpoints:
      - 127.0.0.1:1
`, address, dsn))
	return address
}

func newCompositionServiceContext(t *testing.T, db *sqlx.DB, userClient userv1.UserServiceClient, mailerClient *fakeMailerClient) *svc.ServiceContext {
	t.Helper()
	node, err := snowflake.New()
	require.NoError(t, err)
	tokens, err := token.NewManager(token.Config{
		Issuer:        "cordis.authenticator.integration",
		AccessSecret:  "integration-access-secret-0123456789abcdef",
		RefreshSecret: "integration-refresh-secret-0123456789abcdef",
		AccessTTL:     15 * time.Minute,
		RefreshTTL:    24 * time.Hour,
	})
	require.NoError(t, err)
	cipherKey := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	cipher, err := twofactor.NewCipher("k1", []twofactor.KeyConfig{{ID: "k1", Secret: cipherKey}})
	require.NoError(t, err)

	return svc.NewServiceContextWithDependencies(config.Config{
		Sessions: config.SessionConfig{TTL: 24 * time.Hour},
		Recovery: config.RecoveryConfig{
			PasswordResetTTL:     30 * time.Minute,
			EmailVerificationTTL: 24 * time.Hour,
		},
	}, svc.Dependencies{
		Store:        store.New(db),
		Tokens:       tokens,
		TwoFactor:    cipher,
		Snowflake:    node,
		UserClient:   userClient,
		MailerClient: mailerClient,
	})
}
