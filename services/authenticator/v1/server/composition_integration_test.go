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

	service := New(newCompositionServiceContext(t, db, userClient))
	ctx := t.Context()

	var refreshToken string
	t.Run("register creates user and session", func(t *testing.T) {
		req := new(authenticatorv1.RegisterRequest)
		req.SetName("Alice")
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
`, address, dsn))
	return address
}

func newCompositionServiceContext(t *testing.T, db *sqlx.DB, userClient userv1.UserServiceClient) *svc.ServiceContext {
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
	}, svc.Dependencies{
		Store:      store.New(db),
		Tokens:     tokens,
		TwoFactor:  cipher,
		Snowflake:  node,
		UserClient: userClient,
	})
}
