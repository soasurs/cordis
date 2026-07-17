//go:build integration

package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/migration"
	"github.com/soasurs/cordis/services/api/v1/svc"
	authenticatorMigrations "github.com/soasurs/cordis/services/authenticator/v1/db/migrations"
	guildMigrations "github.com/soasurs/cordis/services/guild/v1/db/migrations"
	messageMigrations "github.com/soasurs/cordis/services/message/v1/db/migrations"
	userMigrations "github.com/soasurs/cordis/services/user/v1/db/migrations"
)

func TestAPIIntegration(t *testing.T) {
	t.Setenv("CORDIS_ACCESS_TOKEN_SECRET", "dev-access-secret-for-integration-test-32b")
	t.Setenv("CORDIS_REFRESH_TOKEN_SECRET", "dev-refresh-secret-for-integration-test-32b")
	// base64-encoded 32-byte AES key for TOTP secret encryption.
	t.Setenv("CORDIS_TOTP_ENCRYPTION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")

	postgres := testkit.StartPostgres(t)
	db, err := database.NewPostgres(database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, migration.Apply(t.Context(), db, userMigrations.Files))
	require.NoError(t, migration.Apply(t.Context(), db, authenticatorMigrations.Files))
	require.NoError(t, migration.Apply(t.Context(), db, guildMigrations.Files))
	require.NoError(t, migration.Apply(t.Context(), db, messageMigrations.Files))

	userAddr := startUser(t, postgres.DSN)
	authAddr := startAuthenticator(t, postgres.DSN, userAddr)
	guildAddr := startGuild(t, postgres.DSN, userAddr)
	messageAddr := startMessage(t, postgres.DSN, guildAddr)

	waitUserReady(t, userAddr)
	waitAuthenticatorReady(t, authAddr)
	waitGuildReady(t, guildAddr)
	waitMessageReady(t, messageAddr)

	authConn := dialGRPC(t, authAddr)
	userConn := dialGRPC(t, userAddr)
	guildConn := dialGRPC(t, guildAddr)
	messageConn := dialGRPC(t, messageAddr)

	apiCtx := &svc.ServiceContext{
		AuthenticatorClient: authenticatorv1.NewAuthenticatorServiceClient(authConn),
		UserClient:          userv1.NewUserServiceClient(userConn),
		GuildClient:         guildv1.NewGuildServiceClient(guildConn),
		MessageClient:       messagev1.NewMessageServiceClient(messageConn),
	}

	mux := http.NewServeMux()
	authPath, authHandler := apiv1connect.NewAuthenticatorServiceHandler(NewAuthenticator(apiCtx))
	mux.Handle(authPath, authHandler)
	userPath, userHandler := apiv1connect.NewUserServiceHandler(NewUser(apiCtx))
	mux.Handle(userPath, userHandler)
	guildPath, guildHandler := apiv1connect.NewGuildServiceHandler(NewGuild(apiCtx))
	mux.Handle(guildPath, guildHandler)
	messagePath, messageHandler := apiv1connect.NewMessageServiceHandler(NewMessage(apiCtx))
	mux.Handle(messagePath, messageHandler)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	httpClient := httpSrv.Client()
	authClient := apiv1connect.NewAuthenticatorServiceClient(httpClient, httpSrv.URL)
	userClient := apiv1connect.NewUserServiceClient(httpClient, httpSrv.URL)

	ctx := t.Context()
	email := "test@integration.example"

	t.Run("register", func(t *testing.T) {
		resp, err := authClient.Register(ctx, &apiv1.RegisterRequest{
			Name:     new("Integration Tester"),
			Email:    new(email),
			Password: new("integration-password-1"),
		})
		require.NoError(t, err)
		require.True(t, resp.GetResult().GetOk())
		require.Positive(t, resp.GetResult().GetUserId())
		require.NotEmpty(t, resp.GetResult().GetAccessToken())
	})

	t.Run("login", func(t *testing.T) {
		resp, err := authClient.Login(ctx, &apiv1.LoginRequest{
			Email:    new(email),
			Password: new("integration-password-1"),
		})
		require.NoError(t, err)
		result := resp.GetResult()
		require.True(t, result.GetOk())
		require.NotEmpty(t, result.GetAccessToken())
	})

	accessToken := func() string {
		resp, err := authClient.Login(ctx, &apiv1.LoginRequest{
			Email:    new(email),
			Password: new("integration-password-1"),
		})
		require.NoError(t, err)
		return resp.GetResult().GetAccessToken()
	}()

	userClientWithToken := apiv1connect.NewUserServiceClient(
		&http.Client{Transport: bearerRoundTripper{base: http.DefaultTransport, accessToken: accessToken}},
		httpSrv.URL,
	)
	guildClientWithToken := apiv1connect.NewGuildServiceClient(
		&http.Client{Transport: bearerRoundTripper{base: http.DefaultTransport, accessToken: accessToken}},
		httpSrv.URL,
	)
	messageClientWithToken := apiv1connect.NewMessageServiceClient(
		&http.Client{Transport: bearerRoundTripper{base: http.DefaultTransport, accessToken: accessToken}},
		httpSrv.URL,
	)

	t.Run("get current user", func(t *testing.T) {
		resp, err := userClientWithToken.GetCurrentUser(ctx, &apiv1.GetCurrentUserRequest{})
		require.NoError(t, err)
		require.Equal(t, email, resp.GetUser().GetEmail())
		require.Equal(t, "Integration Tester", resp.GetProfile().GetName())
	})

	guildID := func() int64 {
		resp, err := guildClientWithToken.CreateGuild(ctx, &apiv1.CreateGuildRequest{
			Name: new("Main Guild"),
		})
		require.NoError(t, err)
		return resp.GetGuild().GetId()
	}()

	t.Run("get guild", func(t *testing.T) {
		resp, err := guildClientWithToken.GetGuild(ctx, &apiv1.GetGuildRequest{GuildId: new(guildID)})
		require.NoError(t, err)
		require.Equal(t, "Main Guild", resp.GetGuild().GetName())
	})

	t.Run("list guilds", func(t *testing.T) {
		resp, err := guildClientWithToken.ListGuilds(ctx, &apiv1.ListGuildsRequest{Limit: new(int32(10))})
		require.NoError(t, err)
		require.NotEmpty(t, resp.GetGuilds())
	})

	channelID := func() int64 {
		resp, err := guildClientWithToken.CreateGuildChannel(ctx, &apiv1.CreateGuildChannelRequest{
			GuildId: new(guildID),
			Name:    new("general"),
			Type:    new(apiv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT),
		})
		require.NoError(t, err)
		return resp.GetChannel().GetId()
	}()

	t.Run("create message", func(t *testing.T) {
		resp, err := messageClientWithToken.CreateMessage(ctx, &apiv1.CreateMessageRequest{
			ChannelId: new(channelID),
			Content:   new("hello world"),
		})
		require.NoError(t, err)
		require.Equal(t, "hello world", resp.GetMessage().GetContent())
	})

	msgID := func() int64 {
		resp, err := messageClientWithToken.CreateMessage(ctx, &apiv1.CreateMessageRequest{
			ChannelId: new(channelID),
			Content:   new("persistent message"),
		})
		require.NoError(t, err)
		return resp.GetMessage().GetId()
	}()

	t.Run("get message", func(t *testing.T) {
		resp, err := messageClientWithToken.GetMessage(ctx, &apiv1.GetMessageRequest{MessageId: new(msgID)})
		require.NoError(t, err)
		require.Equal(t, msgID, resp.GetMessage().GetId())
		require.Equal(t, "persistent message", resp.GetMessage().GetContent())
	})

	t.Run("list messages", func(t *testing.T) {
		resp, err := messageClientWithToken.ListMessages(ctx, &apiv1.ListMessagesRequest{
			ChannelId: new(channelID),
			Limit:     new(int32(25)),
		})
		require.NoError(t, err)
		require.NotEmpty(t, resp.GetMessages())
	})

	t.Run("update message", func(t *testing.T) {
		resp, err := messageClientWithToken.UpdateMessage(ctx, &apiv1.UpdateMessageRequest{
			MessageId: new(msgID),
			Content:   new("updated message"),
		})
		require.NoError(t, err)
		require.Equal(t, "updated message", resp.GetMessage().GetContent())
	})

	t.Run("delete message", func(t *testing.T) {
		resp, err := messageClientWithToken.DeleteMessage(ctx, &apiv1.DeleteMessageRequest{
			MessageId: new(msgID),
		})
		require.NoError(t, err)
		require.True(t, resp.GetOk())
	})

	t.Run("get current user requires token", func(t *testing.T) {
		_, err := userClient.GetCurrentUser(ctx, &apiv1.GetCurrentUserRequest{})
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})

	t.Run("non-member cannot access guild", func(t *testing.T) {
		memberResp, err := authClient.Login(ctx, &apiv1.LoginRequest{
			Email:    new("stranger@example.com"),
			Password: new("stranger-password"),
		})
		if err != nil || !memberResp.GetResult().GetOk() {
			regResp, regErr := authClient.Register(ctx, &apiv1.RegisterRequest{
				Name:     new("Stranger"),
				Email:    new("stranger@example.com"),
				Password: new("stranger-password"),
			})
			require.NoError(t, regErr)
			require.True(t, regResp.GetResult().GetOk())
			memberResp, err = authClient.Login(ctx, &apiv1.LoginRequest{
				Email:    new("stranger@example.com"),
				Password: new("stranger-password"),
			})
			require.NoError(t, err)
		}
		strangerToken := memberResp.GetResult().GetAccessToken()
		strangerClient := apiv1connect.NewGuildServiceClient(
			&http.Client{Transport: bearerRoundTripper{base: http.DefaultTransport, accessToken: strangerToken}},
			httpSrv.URL,
		)
		_, err = strangerClient.GetGuild(ctx, &apiv1.GetGuildRequest{GuildId: new(guildID)})
		require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})

	t.Run("register duplicate email", func(t *testing.T) {
		_, err := authClient.Register(ctx, &apiv1.RegisterRequest{
			Name:     new("Tester 2"),
			Email:    new(email),
			Password: new("another-password"),
		})
		require.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(err))
	})

	t.Run("login wrong password", func(t *testing.T) {
		_, err := authClient.Login(ctx, &apiv1.LoginRequest{
			Email:    new(email),
			Password: new("wrong-password"),
		})
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})
}

func startUser(t *testing.T, dsn string) string {
	t.Helper()
	addr := testkit.FreeAddress(t)
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
`, addr, dsn))
	return addr
}

func startAuthenticator(t *testing.T, dsn, userAddr string) string {
	t.Helper()
	addr := testkit.FreeAddress(t)
	binary := testkit.BuildService(t, "github.com/soasurs/cordis/services/authenticator/v1")
	testkit.StartService(t, binary, fmt.Sprintf(`
name: authenticator.v1
listenOn: %s
timeout: 0
log:
  level: error
  stat: false
database:
  dataSource: %s
tokens:
  issuer: cordis.authenticator.v1
  access:
    secret: ${CORDIS_ACCESS_TOKEN_SECRET}
    ttl: 15m
  refresh:
    secret: ${CORDIS_REFRESH_TOKEN_SECRET}
    ttl: 720h
sessions:
  ttl: 720h
twoFactor:
  issuer: Cordis
  enrollmentTTL: 10m
  loginChallengeTTL: 5m
  maxAttempts: 5
  recoveryCodeCount: 10
  encryption:
    primaryKeyID: totp-test
    keys:
      - id: totp-test
        secret: ${CORDIS_TOTP_ENCRYPTION_KEY}
services:
  user:
    endpoints:
      - %s
`, addr, dsn, userAddr))
	return addr
}

func startGuild(t *testing.T, dsn, userAddr string) string {
	t.Helper()
	addr := testkit.FreeAddress(t)
	binary := testkit.BuildService(t, "github.com/soasurs/cordis/services/guild/v1")
	testkit.StartService(t, binary, fmt.Sprintf(`
name: guild.v1
listenOn: %s
timeout: 0
log:
  level: error
  stat: false
database:
  dataSource: %s
services:
  user:
    endpoints:
      - %s
`, addr, dsn, userAddr))
	return addr
}

func startMessage(t *testing.T, dsn, guildAddr string) string {
	t.Helper()
	addr := testkit.FreeAddress(t)
	binary := testkit.BuildService(t, "github.com/soasurs/cordis/services/message/v1")
	testkit.StartService(t, binary, fmt.Sprintf(`
name: message.v1
listenOn: %s
timeout: 0
log:
  level: error
  stat: false
database:
  dataSource: %s
services:
  guild:
    endpoints:
      - %s
`, addr, dsn, guildAddr))
	return addr
}

func waitUserReady(t *testing.T, address string) {
	t.Helper()
	client := userv1.NewUserServiceClient(dialGRPC(t, address))
	testkit.WaitServiceReady(t, 30*time.Second, func(ctx context.Context) error {
		req := new(userv1.CheckEmailAvailabilityRequest)
		req.SetEmail("probe@example.com")
		_, err := client.CheckEmailAvailability(ctx, req)
		return err
	})
}

func waitAuthenticatorReady(t *testing.T, address string) {
	t.Helper()
	client := authenticatorv1.NewAuthenticatorServiceClient(dialGRPC(t, address))
	testkit.WaitServiceReady(t, 30*time.Second, func(ctx context.Context) error {
		req := new(authenticatorv1.VerifyAccessTokenRequest)
		req.SetAccessToken("probe")
		_, err := client.VerifyAccessToken(ctx, req)
		// A healthy authenticator rejects the fake probe token.
		if status.Code(err) == codes.Unauthenticated {
			return nil
		}
		return err
	})
}

func waitGuildReady(t *testing.T, address string) {
	t.Helper()
	client := guildv1.NewGuildServiceClient(dialGRPC(t, address))
	testkit.WaitServiceReady(t, 30*time.Second, func(ctx context.Context) error {
		req := new(guildv1.AuthorizeGuildChannelRequest)
		req.SetChannelId(1)
		req.SetUserId(1)
		req.SetPermission(uint64(guildv1.GuildPermission_GUILD_PERMISSION_VIEW_CHANNEL))
		_, err := client.AuthorizeGuildChannel(ctx, req)
		// A healthy guild service reports the probe channel as missing.
		if status.Code(err) == codes.NotFound {
			return nil
		}
		return err
	})
}

func waitMessageReady(t *testing.T, address string) {
	t.Helper()
	client := messagev1.NewMessageServiceClient(dialGRPC(t, address))
	testkit.WaitServiceReady(t, 30*time.Second, func(ctx context.Context) error {
		req := new(messagev1.GetMessageRequest)
		req.SetMessageId(1)
		req.SetUserId(1)
		_, err := client.GetMessage(ctx, req)
		// A healthy message service reports the probe message as missing.
		if status.Code(err) == codes.NotFound {
			return nil
		}
		return err
	})
}

func dialGRPC(t *testing.T, address string) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	return conn
}
