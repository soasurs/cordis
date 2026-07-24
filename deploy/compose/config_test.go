package compose_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/conf"

	apiconfig "github.com/soasurs/cordis/services/api/v1/config"
	authenticatorconfig "github.com/soasurs/cordis/services/authenticator/v1/config"
	dispatcherconfig "github.com/soasurs/cordis/services/dispatcher/v1/config"
	gatewayconfig "github.com/soasurs/cordis/services/gateway/v1/config"
	guildconfig "github.com/soasurs/cordis/services/guild/v1/config"
	mailerconfig "github.com/soasurs/cordis/services/mailer/v1/config"
	mediaconfig "github.com/soasurs/cordis/services/media/v1/config"
	messageconfig "github.com/soasurs/cordis/services/message/v1/config"
	presenceconfig "github.com/soasurs/cordis/services/presence/v1/config"
	sessionconfig "github.com/soasurs/cordis/services/session/v1/config"
	userconfig "github.com/soasurs/cordis/services/user/v1/config"
)

func TestServiceConfigsLoad(t *testing.T) {
	t.Setenv("CORDIS_DATABASE_DSN", "postgres://cordis:test@postgres:5432/cordis?sslmode=disable")
	t.Setenv("CORDIS_ACCESS_TOKEN_SECRET", "test-access-secret")
	t.Setenv("CORDIS_REFRESH_TOKEN_SECRET", "test-refresh-secret")
	t.Setenv("CORDIS_TOTP_ENCRYPTION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	t.Setenv("CORDIS_MINIO_ROOT_USER", "cordislocal")
	t.Setenv("CORDIS_MINIO_ROOT_PASSWORD", "cordislocalpass")
	t.Setenv("CORDIS_MEDIA_OBJECT_STORE_ENDPOINT", "storage.cordis.localhost:9000")
	t.Setenv("CORDIS_GATEWAY_ORIGIN", "http://localhost:5173")
	t.Setenv("CORDIS_EMAIL_VERIFICATION_URL", "http://localhost:5173/verify-email")
	t.Setenv("CORDIS_PASSWORD_RESET_URL", "http://localhost:5173/reset-password")

	loadConfig(t, "api.yaml", new(apiconfig.Config))
	loadConfig(t, "dispatcher.yaml", new(dispatcherconfig.Config))
	gateway := new(gatewayconfig.Config)
	loadConfig(t, "gateway.yaml", gateway)
	require.Equal(t, "/", gateway.Gateway.WebSocketRoute())
	require.Equal(t, []string{"http://localhost:5173"}, gateway.Gateway.OriginPatterns)

	authenticator := new(authenticatorconfig.Config)
	guild := new(guildconfig.Config)
	mailer := new(mailerconfig.Config)
	media := new(mediaconfig.Config)
	message := new(messageconfig.Config)
	presence := new(presenceconfig.Config)
	session := new(sessionconfig.Config)
	user := new(userconfig.Config)
	rpcConfigs := []struct {
		name        string
		target      any
		statEnabled func() bool
	}{
		{name: "authenticator.yaml", target: authenticator, statEnabled: func() bool { return authenticator.Middlewares.Stat }},
		{name: "guild.yaml", target: guild, statEnabled: func() bool { return guild.Middlewares.Stat }},
		{name: "mailer.yaml", target: mailer, statEnabled: func() bool { return mailer.Middlewares.Stat }},
		{name: "media.yaml", target: media, statEnabled: func() bool { return media.Middlewares.Stat }},
		{name: "message.yaml", target: message, statEnabled: func() bool { return message.Middlewares.Stat }},
		{name: "presence.yaml", target: presence, statEnabled: func() bool { return presence.Middlewares.Stat }},
		{name: "session.yaml", target: session, statEnabled: func() bool { return session.Middlewares.Stat }},
		{name: "user.yaml", target: user, statEnabled: func() bool { return user.Middlewares.Stat }},
	}
	for _, cfg := range rpcConfigs {
		loadConfig(t, cfg.name, cfg.target)
		require.False(t, cfg.statEnabled(), cfg.name)
	}
	require.Equal(t, "smtp", mailer.Mailer.Provider)
	require.Equal(t, "mailpit:1025", mailer.Mailer.SMTP.Address)
	require.False(t, mailer.Mailer.SMTP.RequireTLS)
	require.Equal(t, "http://localhost:5173/verify-email", mailer.Mailer.EmailVerificationURL)
	require.NoError(t, media.Validate())
}

func loadConfig(t *testing.T, name string, target any) {
	t.Helper()
	require.NoError(t, conf.LoadConfig(filepath.Join("config", name), target, conf.UseEnv()))
}
