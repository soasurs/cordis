//go:build integration

package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	mailerv1 "github.com/soasurs/cordis/gen/mailer/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/internal/testkit"
)

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
cursor:
  secret: test-cursor-secret-at-least-32-bytes!
services:
  media:
    endpoints:
      - 127.0.0.1:1
`, addr, dsn))
	return addr
}

func startAuthenticator(t *testing.T, dsn, redisAddr, userAddr, mailerAddr string) string {
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
  idleTTL: 720h
  absoluteTTL: 4320h
  rotationGrace: 30s
gatewayTickets:
  ttl: 30s
  keyPrefix: "cordis:test:gateway_ticket:"
  redis:
    host: %s
    type: node
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
`, addr, dsn, redisAddr, userAddr, mailerAddr))
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
cursor:
  secret: test-cursor-secret-at-least-32-bytes!
services:
  user:
    endpoints:
      - %s
  media:
    endpoints:
      - 127.0.0.1:1
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
cursor:
  secret: test-cursor-secret-at-least-32-bytes!
services:
  guild:
    endpoints:
      - %s
  user:
    endpoints:
      - %s
  media:
    endpoints:
      - 127.0.0.1:1
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
