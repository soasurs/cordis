//go:build integration

package server

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/migration"
	"github.com/soasurs/cordis/pkg/snowflake"
	guildmigrations "github.com/soasurs/cordis/services/guild/v1/db/migrations"
	"github.com/soasurs/cordis/services/message/v1/config"
	messagemigrations "github.com/soasurs/cordis/services/message/v1/db/migrations"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
	"github.com/soasurs/cordis/services/message/v1/internal/svc"
	usermigrations "github.com/soasurs/cordis/services/user/v1/db/migrations"
)

// TestMessageGuildUserComposition runs the Message server in-process against
// real User and Guild service binaries so that channel authorization and
// member verification cross real gRPC and PostgreSQL instead of fakes.
func TestMessageGuildUserComposition(t *testing.T) {
	postgres := testkit.StartPostgres(t)
	db, err := database.NewPostgres(database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, migration.Apply(t.Context(), db, usermigrations.Files))
	require.NoError(t, migration.Apply(t.Context(), db, guildmigrations.Files))
	require.NoError(t, migration.Apply(t.Context(), db, messagemigrations.Files))

	userAddress := startUserService(t, postgres.DSN)
	guildAddress := startGuildService(t, postgres.DSN, userAddress)
	userClient := userv1.NewUserServiceClient(dialService(t, userAddress))
	guildClient := guildv1.NewGuildServiceClient(dialService(t, guildAddress))

	node, err := snowflake.New()
	require.NoError(t, err)
	messageService := New(svc.NewServiceContextWithDependencies(config.Config{}, svc.Dependencies{
		Store:       store.New(db),
		Snowflake:   node,
		GuildClient: guildClient,
		UserClient:  userClient,
	}))

	ctx := t.Context()
	ownerID := createUser(t, userClient, "owner@example.com")
	memberID := createUser(t, userClient, "member@example.com")

	createGuild := new(guildv1.CreateGuildRequest)
	createGuild.SetOwnerId(ownerID)
	createGuild.SetName("Cordis")
	guildResp, err := guildClient.CreateGuild(ctx, createGuild)
	require.NoError(t, err)
	guildID := guildResp.GetGuild().GetId()

	textChannelID := createChannel(t, guildClient, guildID, ownerID, "general", guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT)
	categoryChannelID := createChannel(t, guildClient, guildID, ownerID, "rooms", guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_CATEGORY)

	t.Run("guild verifies member through real user service", func(t *testing.T) {
		add := new(guildv1.AddGuildMemberRequest)
		add.SetGuildId(guildID)
		add.SetActorUserId(ownerID)
		add.SetUserId(memberID)
		_, err := guildClient.AddGuildMember(ctx, add)
		require.NoError(t, err)

		add = new(guildv1.AddGuildMemberRequest)
		add.SetGuildId(guildID)
		add.SetActorUserId(ownerID)
		add.SetUserId(999999)
		_, err = guildClient.AddGuildMember(ctx, add)
		require.Equal(t, codes.NotFound, status.Code(err))
	})

	var ownerMessageID int64
	t.Run("authorized members can create messages", func(t *testing.T) {
		created := createMessage(t, messageService, textChannelID, ownerID, "hello from owner")
		ownerMessageID = created.GetMessage().GetId()
		createMessage(t, messageService, textChannelID, memberID, "hello from member")
	})

	t.Run("non-member sees channel as not found", func(t *testing.T) {
		req := new(messagev1.CreateMessageRequest)
		req.SetChannelId(textChannelID)
		req.SetAuthorId(888888)
		req.SetContent("intruder")
		_, err := messageService.CreateMessage(ctx, req)
		require.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("category channels reject messages", func(t *testing.T) {
		req := new(messagev1.CreateMessageRequest)
		req.SetChannelId(categoryChannelID)
		req.SetAuthorId(ownerID)
		req.SetContent("misplaced")
		_, err := messageService.CreateMessage(ctx, req)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("non-author edit requires manage messages", func(t *testing.T) {
		req := new(messagev1.UpdateMessageRequest)
		req.SetMessageId(ownerMessageID)
		req.SetActorUserId(memberID)
		req.SetContent("hijacked")
		_, err := messageService.UpdateMessage(ctx, req)
		require.Equal(t, codes.PermissionDenied, status.Code(err))
	})

	t.Run("revoking send messages denies member but not owner", func(t *testing.T) {
		update := new(guildv1.UpdateGuildRoleRequest)
		update.SetGuildId(guildID)
		update.SetActorUserId(ownerID)
		update.SetRoleId(guildID)
		update.SetPermissions(uint64(guildv1.GuildPermission_GUILD_PERMISSION_VIEW_CHANNEL))
		_, err := guildClient.UpdateGuildRole(ctx, update)
		require.NoError(t, err)

		req := new(messagev1.CreateMessageRequest)
		req.SetChannelId(textChannelID)
		req.SetAuthorId(memberID)
		req.SetContent("silenced")
		_, err = messageService.CreateMessage(ctx, req)
		require.Equal(t, codes.PermissionDenied, status.Code(err))

		createMessage(t, messageService, textChannelID, ownerID, "owner bypasses")
	})

	t.Run("direct messages", func(t *testing.T) {
		aliceID := createUser(t, userClient, "alice-dm@example.com")
		bobID := createUser(t, userClient, "bob-dm@example.com")
		carolID := createUser(t, userClient, "carol-dm@example.com")

		sendFriend := func(from, to int64) {
			req := new(userv1.SendFriendRequestRequest)
			req.SetUserId(from)
			req.SetTargetId(to)
			_, err := userClient.SendFriendRequest(t.Context(), req)
			require.NoError(t, err)
		}
		acceptFriend := func(from, to int64) {
			req := new(userv1.AcceptFriendRequestRequest)
			req.SetUserId(from)
			req.SetTargetId(to)
			_, err := userClient.AcceptFriendRequest(t.Context(), req)
			require.NoError(t, err)
		}

		sendFriend(aliceID, bobID)
		acceptFriend(bobID, aliceID)

		t.Run("non-friend cannot open dm", func(t *testing.T) {
			req := new(messagev1.CreateDmChannelRequest)
			req.SetUserId(carolID)
			req.SetTargetId(aliceID)
			_, err := messageService.CreateDmChannel(t.Context(), req)
			require.Equal(t, codes.PermissionDenied, status.Code(err))
		})

		var dmChannelID int64

		t.Run("friend opens dm", func(t *testing.T) {
			req := new(messagev1.CreateDmChannelRequest)
			req.SetUserId(aliceID)
			req.SetTargetId(bobID)
			resp, err := messageService.CreateDmChannel(t.Context(), req)
			require.NoError(t, err)
			dmChannel := resp.GetChannel()
			require.Positive(t, dmChannel.GetId())
			dmChannelID = dmChannel.GetId()

			resp2, err := messageService.CreateDmChannel(t.Context(), req)
			require.NoError(t, err)
			require.Equal(t, dmChannelID, resp2.GetChannel().GetId())
		})

		var aliceMsgID int64

		t.Run("messages in dm channel", func(t *testing.T) {
			createReq := new(messagev1.CreateMessageRequest)
			createReq.SetChannelId(dmChannelID)
			createReq.SetAuthorId(aliceID)
			createReq.SetContent("hello bob")
			createResp, err := messageService.CreateMessage(t.Context(), createReq)
			require.NoError(t, err)
			aliceMsgID = createResp.GetMessage().GetId()

			getReq := new(messagev1.GetMessageRequest)
			getReq.SetMessageId(aliceMsgID)
			getReq.SetUserId(bobID)
			_, err = messageService.GetMessage(t.Context(), getReq)
			require.NoError(t, err)

			listReq := new(messagev1.ListMessagesRequest)
			listReq.SetChannelId(dmChannelID)
			listReq.SetUserId(bobID)
			_, err = messageService.ListMessages(t.Context(), listReq)
			require.NoError(t, err)

			getReq = new(messagev1.GetMessageRequest)
			getReq.SetMessageId(aliceMsgID)
			getReq.SetUserId(carolID)
			_, err = messageService.GetMessage(t.Context(), getReq)
			require.Equal(t, codes.NotFound, status.Code(err))
		})

		t.Run("non-author edit denied in dm", func(t *testing.T) {
			updateReq := new(messagev1.UpdateMessageRequest)
			updateReq.SetMessageId(aliceMsgID)
			updateReq.SetActorUserId(bobID)
			updateReq.SetContent("hijacked by bob")
			_, err := messageService.UpdateMessage(t.Context(), updateReq)
			require.Equal(t, codes.PermissionDenied, status.Code(err))
		})

		t.Run("block prevents sending", func(t *testing.T) {
			blockReq := new(userv1.BlockUserRequest)
			blockReq.SetUserId(bobID)
			blockReq.SetTargetId(aliceID)
			_, err := userClient.BlockUser(t.Context(), blockReq)
			require.NoError(t, err)

			createReq := new(messagev1.CreateMessageRequest)
			createReq.SetChannelId(dmChannelID)
			createReq.SetAuthorId(aliceID)
			createReq.SetContent("blocked")
			_, err = messageService.CreateMessage(t.Context(), createReq)
			require.Equal(t, codes.PermissionDenied, status.Code(err))

			createReq = new(messagev1.CreateMessageRequest)
			createReq.SetChannelId(dmChannelID)
			createReq.SetAuthorId(bobID)
			createReq.SetContent("i blocked you")
			_, err = messageService.CreateMessage(t.Context(), createReq)
			require.Equal(t, codes.PermissionDenied, status.Code(err))

			listReq := new(messagev1.ListMessagesRequest)
			listReq.SetChannelId(dmChannelID)
			listReq.SetUserId(bobID)
			listResp, err := messageService.ListMessages(t.Context(), listReq)
			require.NoError(t, err)
			require.Len(t, listResp.GetMessages(), 1)
		})

		t.Run("unblock restores sending", func(t *testing.T) {
			unblockReq := new(userv1.UnblockUserRequest)
			unblockReq.SetUserId(bobID)
			unblockReq.SetTargetId(aliceID)
			_, err := userClient.UnblockUser(t.Context(), unblockReq)
			require.NoError(t, err)

			createReq := new(messagev1.CreateMessageRequest)
			createReq.SetChannelId(dmChannelID)
			createReq.SetAuthorId(aliceID)
			createReq.SetContent("back to normal")
			_, err = messageService.CreateMessage(t.Context(), createReq)
			require.NoError(t, err)
		})
	})
}

func startUserService(t *testing.T, dsn string) string {
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
	waitUserReady(t, address)
	return address
}

func startGuildService(t *testing.T, dsn, userAddress string) string {
	t.Helper()
	address := testkit.FreeAddress(t)
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
`, address, dsn, userAddress))
	waitGuildReady(t, address)
	return address
}

func dialService(t *testing.T, address string) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	return conn
}

func waitUserReady(t *testing.T, address string) {
	t.Helper()
	client := userv1.NewUserServiceClient(dialService(t, address))
	testkit.WaitServiceReady(t, 30*time.Second, func(ctx context.Context) error {
		req := new(userv1.CheckEmailAvailabilityRequest)
		req.SetEmail("probe@example.com")
		_, err := client.CheckEmailAvailability(ctx, req)
		return err
	})
}

func waitGuildReady(t *testing.T, address string) {
	t.Helper()
	client := guildv1.NewGuildServiceClient(dialService(t, address))
	testkit.WaitServiceReady(t, 30*time.Second, func(ctx context.Context) error {
		req := new(guildv1.AuthorizeGuildChannelRequest)
		req.SetChannelId(1)
		req.SetUserId(1)
		req.SetPermission(uint64(guildv1.GuildPermission_GUILD_PERMISSION_VIEW_CHANNEL))
		_, err := client.AuthorizeGuildChannel(ctx, req)
		if status.Code(err) == codes.NotFound {
			return nil
		}
		return err
	})
}

func createUser(t *testing.T, client userv1.UserServiceClient, email string) int64 {
	t.Helper()
	req := new(userv1.CreateUserRequest)
	req.SetName("Tester")
	req.SetUsername(strings.ReplaceAll(strings.SplitN(email, "@", 2)[0], "-", "_"))
	req.SetEmail(email)
	resp, err := client.CreateUser(t.Context(), req)
	require.NoError(t, err)
	userID := resp.GetUser().GetUserId()
	require.Positive(t, userID)
	return userID
}

func createChannel(
	t *testing.T,
	client guildv1.GuildServiceClient,
	guildID, actorID int64,
	name string,
	channelType guildv1.GuildChannelType,
) int64 {
	t.Helper()
	req := new(guildv1.CreateGuildChannelRequest)
	req.SetGuildId(guildID)
	req.SetActorUserId(actorID)
	req.SetName(name)
	req.SetType(channelType)
	resp, err := client.CreateGuildChannel(t.Context(), req)
	require.NoError(t, err)
	return resp.GetChannel().GetId()
}

func createMessage(
	t *testing.T,
	service messagev1.MessageServiceServer,
	channelID, authorID int64,
	content string,
) *messagev1.CreateMessageResponse {
	t.Helper()
	req := new(messagev1.CreateMessageRequest)
	req.SetChannelId(channelID)
	req.SetAuthorId(authorID)
	req.SetContent(content)
	resp, err := service.CreateMessage(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, content, resp.GetMessage().GetContent())
	require.Equal(t, authorID, resp.GetMessage().GetAuthor().GetUserId())
	return resp
}
