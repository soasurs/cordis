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
	mailerv1 "github.com/soasurs/cordis/gen/mailer/v1"
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
	mailerAddr := startMailer(t)
	authAddr := startAuthenticator(t, postgres.DSN, userAddr, mailerAddr)
	guildAddr := startGuild(t, postgres.DSN, userAddr)
	messageAddr := startMessage(t, postgres.DSN, guildAddr, userAddr)

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
		req := new(apiv1.RegisterRequest)
		req.SetName("Integration Tester")
		req.SetUsername("integration_tester")
		req.SetEmail(email)
		req.SetPassword("integration-password-1")
		resp, err := authClient.Register(ctx, req)
		require.NoError(t, err)
		require.True(t, resp.GetResult().GetOk())
		require.Positive(t, resp.GetResult().GetUserId())
		require.NotEmpty(t, resp.GetResult().GetAccessToken())
	})

	t.Run("password reset request reaches mailer", func(t *testing.T) {
		// End-to-end wiring check for authenticator -> mailer: the noop
		// provider drops the mail, so success only proves the gRPC path.
		req := new(apiv1.RequestPasswordResetRequest)
		req.SetEmail(email)
		resp, err := authClient.RequestPasswordReset(ctx, req)
		require.NoError(t, err)
		require.True(t, resp.GetOk())

		// Unknown addresses are indistinguishable from known ones.
		req2 := new(apiv1.RequestPasswordResetRequest)
		req2.SetEmail("unknown@integration.example")
		resp, err = authClient.RequestPasswordReset(ctx, req2)
		require.NoError(t, err)
		require.True(t, resp.GetOk())
	})

	t.Run("login", func(t *testing.T) {
		req := new(apiv1.LoginRequest)
		req.SetEmail(email)
		req.SetPassword("integration-password-1")
		resp, err := authClient.Login(ctx, req)
		require.NoError(t, err)
		result := resp.GetResult()
		require.True(t, result.GetOk())
		require.NotEmpty(t, result.GetAccessToken())
	})

	accessToken := func() string {
		req := new(apiv1.LoginRequest)
		req.SetEmail(email)
		req.SetPassword("integration-password-1")
		resp, err := authClient.Login(ctx, req)
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
		resp, err := userClientWithToken.GetCurrentUser(ctx, new(apiv1.GetCurrentUserRequest))
		require.NoError(t, err)
		require.Equal(t, email, resp.GetUser().GetEmail())
		require.Equal(t, "Integration Tester", resp.GetProfile().GetName())
	})

	guildID := func() int64 {
		req := new(apiv1.CreateGuildRequest)
		req.SetName("Main Guild")
		resp, err := guildClientWithToken.CreateGuild(ctx, req)
		require.NoError(t, err)
		return resp.GetGuild().GetId()
	}()

	t.Run("get guild", func(t *testing.T) {
		req := new(apiv1.GetGuildRequest)
		req.SetGuildId(guildID)
		resp, err := guildClientWithToken.GetGuild(ctx, req)
		require.NoError(t, err)
		require.Equal(t, "Main Guild", resp.GetGuild().GetName())
	})

	t.Run("list guilds", func(t *testing.T) {
		req := new(apiv1.ListGuildsRequest)
		req.SetLimit(10)
		resp, err := guildClientWithToken.ListGuilds(ctx, req)
		require.NoError(t, err)
		require.NotEmpty(t, resp.GetGuilds())
	})

	channelID := func() int64 {
		req := new(apiv1.CreateGuildChannelRequest)
		req.SetGuildId(guildID)
		req.SetName("general")
		req.SetType(apiv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT)
		resp, err := guildClientWithToken.CreateGuildChannel(ctx, req)
		require.NoError(t, err)
		return resp.GetChannel().GetId()
	}()

	t.Run("create message", func(t *testing.T) {
		req := new(apiv1.CreateMessageRequest)
		req.SetChannelId(channelID)
		req.SetContent("hello world")
		resp, err := messageClientWithToken.CreateMessage(ctx, req)
		require.NoError(t, err)
		require.Equal(t, "hello world", resp.GetMessage().GetContent())
	})

	msgID := func() int64 {
		req := new(apiv1.CreateMessageRequest)
		req.SetChannelId(channelID)
		req.SetContent("persistent message")
		resp, err := messageClientWithToken.CreateMessage(ctx, req)
		require.NoError(t, err)
		return resp.GetMessage().GetId()
	}()

	t.Run("get message", func(t *testing.T) {
		req := new(apiv1.GetMessageRequest)
		req.SetMessageId(msgID)
		resp, err := messageClientWithToken.GetMessage(ctx, req)
		require.NoError(t, err)
		require.Equal(t, msgID, resp.GetMessage().GetId())
		require.Equal(t, "persistent message", resp.GetMessage().GetContent())
	})

	t.Run("list messages", func(t *testing.T) {
		req := new(apiv1.ListMessagesRequest)
		req.SetChannelId(channelID)
		req.SetLimit(25)
		resp, err := messageClientWithToken.ListMessages(ctx, req)
		require.NoError(t, err)
		require.NotEmpty(t, resp.GetMessages())
	})

	t.Run("update message", func(t *testing.T) {
		req := new(apiv1.UpdateMessageRequest)
		req.SetMessageId(msgID)
		req.SetContent("updated message")
		resp, err := messageClientWithToken.UpdateMessage(ctx, req)
		require.NoError(t, err)
		require.Equal(t, "updated message", resp.GetMessage().GetContent())
	})

	t.Run("delete message", func(t *testing.T) {
		req := new(apiv1.DeleteMessageRequest)
		req.SetMessageId(msgID)
		resp, err := messageClientWithToken.DeleteMessage(ctx, req)
		require.NoError(t, err)
		require.True(t, resp.GetOk())
	})

	t.Run("get current user requires token", func(t *testing.T) {
		_, err := userClient.GetCurrentUser(ctx, new(apiv1.GetCurrentUserRequest))
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})

	t.Run("non-member cannot access guild", func(t *testing.T) {
		loginReq := new(apiv1.LoginRequest)
		loginReq.SetEmail("stranger@example.com")
		loginReq.SetPassword("stranger-password")
		memberResp, err := authClient.Login(ctx, loginReq)
		if err != nil || !memberResp.GetResult().GetOk() {
			regReq := new(apiv1.RegisterRequest)
			regReq.SetName("Stranger")
			regReq.SetUsername("stranger")
			regReq.SetEmail("stranger@example.com")
			regReq.SetPassword("stranger-password")
			regResp, regErr := authClient.Register(ctx, regReq)
			require.NoError(t, regErr)
			require.True(t, regResp.GetResult().GetOk())
			loginReq2 := new(apiv1.LoginRequest)
			loginReq2.SetEmail("stranger@example.com")
			loginReq2.SetPassword("stranger-password")
			memberResp, err = authClient.Login(ctx, loginReq2)
			require.NoError(t, err)
		}
		strangerToken := memberResp.GetResult().GetAccessToken()
		strangerClient := apiv1connect.NewGuildServiceClient(
			&http.Client{Transport: bearerRoundTripper{base: http.DefaultTransport, accessToken: strangerToken}},
			httpSrv.URL,
		)
		getReq := new(apiv1.GetGuildRequest)
		getReq.SetGuildId(guildID)
		_, err = strangerClient.GetGuild(ctx, getReq)
		require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})

	t.Run("register duplicate email", func(t *testing.T) {
		req := new(apiv1.RegisterRequest)
		req.SetName("Tester 2")
		req.SetUsername("tester_two")
		req.SetEmail(email)
		req.SetPassword("another-password")
		_, err := authClient.Register(ctx, req)
		require.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(err))
	})

	t.Run("login wrong password", func(t *testing.T) {
		req := new(apiv1.LoginRequest)
		req.SetEmail(email)
		req.SetPassword("wrong-password")
		_, err := authClient.Login(ctx, req)
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})

	t.Run("friendship lifecycle", func(t *testing.T) {
		loginReq := new(apiv1.LoginRequest)
		loginReq.SetEmail("stranger@example.com")
		loginReq.SetPassword("stranger-password")
		strangerResp, err := authClient.Login(ctx, loginReq)
		if err != nil || !strangerResp.GetResult().GetOk() {
			regReq := new(apiv1.RegisterRequest)
			regReq.SetName("Stranger")
			regReq.SetUsername("stranger")
			regReq.SetEmail("stranger@example.com")
			regReq.SetPassword("stranger-password")
			regResp, regErr := authClient.Register(ctx, regReq)
			require.NoError(t, regErr)
			require.True(t, regResp.GetResult().GetOk())
			loginReq2 := new(apiv1.LoginRequest)
			loginReq2.SetEmail("stranger@example.com")
			loginReq2.SetPassword("stranger-password")
			strangerResp, err = authClient.Login(ctx, loginReq2)
			require.NoError(t, err)
		}
		strangerUserClient := apiv1connect.NewUserServiceClient(
			&http.Client{Transport: bearerRoundTripper{base: http.DefaultTransport, accessToken: strangerResp.GetResult().GetAccessToken()}},
			httpSrv.URL,
		)

		// The unique handle resolves to the target across real services.
		lookupReq := new(apiv1.LookupUserRequest)
		lookupReq.SetUsername("STRANGER")
		lookupResp, err := userClientWithToken.LookupUser(ctx, lookupReq)
		require.NoError(t, err)
		strangerID := lookupResp.GetProfile().GetUserId()
		require.Positive(t, strangerID)
		require.Equal(t, "stranger", lookupResp.GetProfile().GetUsername())

		meResp, err := userClientWithToken.GetCurrentUser(ctx, new(apiv1.GetCurrentUserRequest))
		require.NoError(t, err)
		testerID := meResp.GetUser().GetUserId()

		sendReq := new(apiv1.SendFriendRequestRequest)
		sendReq.SetTargetId(strangerID)
		sendResp, err := userClientWithToken.SendFriendRequest(ctx, sendReq)
		require.NoError(t, err)
		require.Equal(t, apiv1.RelationshipType_RELATIONSHIP_TYPE_OUTGOING, sendResp.GetRelationship().GetType())

		listResp, err := strangerUserClient.ListRelationships(ctx, new(apiv1.ListRelationshipsRequest))
		require.NoError(t, err)
		require.Len(t, listResp.GetRelationships(), 1)
		require.Equal(t, testerID, listResp.GetRelationships()[0].GetTargetId())
		require.Equal(t, apiv1.RelationshipType_RELATIONSHIP_TYPE_INCOMING, listResp.GetRelationships()[0].GetType())

		acceptReq := new(apiv1.AcceptFriendRequestRequest)
		acceptReq.SetTargetId(testerID)
		acceptResp, err := strangerUserClient.AcceptFriendRequest(ctx, acceptReq)
		require.NoError(t, err)
		require.Equal(t, apiv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND, acceptResp.GetRelationship().GetType())

		listReq := new(apiv1.ListRelationshipsRequest)
		listReq.SetType(apiv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND)
		listResp, err = userClientWithToken.ListRelationships(ctx, listReq)
		require.NoError(t, err)
		require.Len(t, listResp.GetRelationships(), 1)
		require.Equal(t, strangerID, listResp.GetRelationships()[0].GetTargetId())

		// Blocking strips the friendship and further requests are refused.
		blockReq := new(apiv1.BlockUserRequest)
		blockReq.SetTargetId(strangerID)
		blockResp, err := userClientWithToken.BlockUser(ctx, blockReq)
		require.NoError(t, err)
		require.Equal(t, apiv1.RelationshipType_RELATIONSHIP_TYPE_BLOCKED, blockResp.GetRelationship().GetType())

		listResp, err = strangerUserClient.ListRelationships(ctx, new(apiv1.ListRelationshipsRequest))
		require.NoError(t, err)
		require.Empty(t, listResp.GetRelationships())

		sendReq2 := new(apiv1.SendFriendRequestRequest)
		sendReq2.SetTargetId(testerID)
		_, err = strangerUserClient.SendFriendRequest(ctx, sendReq2)
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

		unblockReq := new(apiv1.UnblockUserRequest)
		unblockReq.SetTargetId(strangerID)
		unblockResp, err := userClientWithToken.UnblockUser(ctx, unblockReq)
		require.NoError(t, err)
		require.True(t, unblockResp.GetOk())
	})

	t.Run("direct message lifecycle", func(t *testing.T) {
		loginReq := new(apiv1.LoginRequest)
		loginReq.SetEmail("stranger@example.com")
		loginReq.SetPassword("stranger-password")
		strangerResp, err := authClient.Login(ctx, loginReq)
		require.NoError(t, err)
		strangerTransport := &http.Client{Transport: bearerRoundTripper{base: http.DefaultTransport, accessToken: strangerResp.GetResult().GetAccessToken()}}
		strangerUserClient := apiv1connect.NewUserServiceClient(strangerTransport, httpSrv.URL)
		strangerMessageClient := apiv1connect.NewMessageServiceClient(strangerTransport, httpSrv.URL)

		lookupReq := new(apiv1.LookupUserRequest)
		lookupReq.SetUsername("stranger")
		lookupResp, err := userClientWithToken.LookupUser(ctx, lookupReq)
		require.NoError(t, err)
		strangerID := lookupResp.GetProfile().GetUserId()
		meResp, err := userClientWithToken.GetCurrentUser(ctx, new(apiv1.GetCurrentUserRequest))
		require.NoError(t, err)
		testerID := meResp.GetUser().GetUserId()

		// DMs require friendship: the earlier block stripped it.
		dmReq := new(apiv1.CreateDmChannelRequest)
		dmReq.SetTargetId(strangerID)
		_, err = messageClientWithToken.CreateDmChannel(ctx, dmReq)
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

		sendReq := new(apiv1.SendFriendRequestRequest)
		sendReq.SetTargetId(strangerID)
		_, err = userClientWithToken.SendFriendRequest(ctx, sendReq)
		require.NoError(t, err)
		acceptReq := new(apiv1.AcceptFriendRequestRequest)
		acceptReq.SetTargetId(testerID)
		_, err = strangerUserClient.AcceptFriendRequest(ctx, acceptReq)
		require.NoError(t, err)

		channelReq := new(apiv1.CreateDmChannelRequest)
		channelReq.SetTargetId(strangerID)
		channelResp, err := messageClientWithToken.CreateDmChannel(ctx, channelReq)
		require.NoError(t, err)
		dmChannelID := channelResp.GetChannel().GetId()
		require.Equal(t, strangerID, channelResp.GetChannel().GetRecipientId())

		// Idempotent reopen from the other side lands on the same channel.
		reopenReq := new(apiv1.CreateDmChannelRequest)
		reopenReq.SetTargetId(testerID)
		reopenResp, err := strangerMessageClient.CreateDmChannel(ctx, reopenReq)
		require.NoError(t, err)
		require.Equal(t, dmChannelID, reopenResp.GetChannel().GetId())
		require.Equal(t, testerID, reopenResp.GetChannel().GetRecipientId())

		sendMsgReq := new(apiv1.CreateMessageRequest)
		sendMsgReq.SetChannelId(dmChannelID)
		sendMsgReq.SetContent("hello dm")
		sendResp, err := messageClientWithToken.CreateMessage(ctx, sendMsgReq)
		require.NoError(t, err)
		require.Equal(t, "hello dm", sendResp.GetMessage().GetContent())

		listMsgReq := new(apiv1.ListMessagesRequest)
		listMsgReq.SetChannelId(dmChannelID)
		listMessagesResp, err := strangerMessageClient.ListMessages(ctx, listMsgReq)
		require.NoError(t, err)
		require.Len(t, listMessagesResp.GetMessages(), 1)

		listChannelsResp, err := strangerMessageClient.ListDmChannels(ctx, new(apiv1.ListDmChannelsRequest))
		require.NoError(t, err)
		require.Len(t, listChannelsResp.GetChannels(), 1)
		require.Equal(t, testerID, listChannelsResp.GetChannels()[0].GetRecipientId())

		// A block freezes writing in both directions but keeps history readable.
		blockReq := new(apiv1.BlockUserRequest)
		blockReq.SetTargetId(testerID)
		_, err = strangerUserClient.BlockUser(ctx, blockReq)
		require.NoError(t, err)
		msgReq1 := new(apiv1.CreateMessageRequest)
		msgReq1.SetChannelId(dmChannelID)
		msgReq1.SetContent("blocked?")
		_, err = messageClientWithToken.CreateMessage(ctx, msgReq1)
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		msgReq2 := new(apiv1.CreateMessageRequest)
		msgReq2.SetChannelId(dmChannelID)
		msgReq2.SetContent("me neither")
		_, err = strangerMessageClient.CreateMessage(ctx, msgReq2)
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		listMsgReq2 := new(apiv1.ListMessagesRequest)
		listMsgReq2.SetChannelId(dmChannelID)
		listMessagesResp, err = messageClientWithToken.ListMessages(ctx, listMsgReq2)
		require.NoError(t, err)
		require.Len(t, listMessagesResp.GetMessages(), 1)
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

func startAuthenticator(t *testing.T, dsn, userAddr, mailerAddr string) string {
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
  mailer:
    endpoints:
      - %s
`, addr, dsn, userAddr, mailerAddr))
	return addr
}

func startMailer(t *testing.T) string {
	t.Helper()
	addr := testkit.FreeAddress(t)
	binary := testkit.BuildService(t, "github.com/soasurs/cordis/services/mailer/v1")
	testkit.StartService(t, binary, fmt.Sprintf(`
name: mailer.v1
listenOn: %s
timeout: 0
log:
  level: error
  stat: false
mailer:
  provider: noop
`, addr))
	waitMailerReady(t, addr)
	return addr
}

func waitMailerReady(t *testing.T, address string) {
	t.Helper()
	client := mailerv1.NewMailerServiceClient(dialGRPC(t, address))
	testkit.WaitServiceReady(t, 30*time.Second, func(ctx context.Context) error {
		req := new(mailerv1.SendEmailRequest)
		req.SetTemplate("probe")
		_, err := client.SendEmail(ctx, req)
		// A healthy mailer rejects the incomplete probe request.
		if status.Code(err) == codes.InvalidArgument {
			return nil
		}
		return err
	})
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

func startMessage(t *testing.T, dsn, guildAddr, userAddr string) string {
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
  user:
    endpoints:
      - %s
`, addr, dsn, guildAddr, userAddr))
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
