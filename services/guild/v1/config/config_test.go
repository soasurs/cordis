package config

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/conf"
)

func TestLoadConfig(t *testing.T) {
	t.Setenv("CORDIS_CURSOR_SECRET", "test-cursor-secret-at-least-32-bytes!")
	var cfg Config
	require.NoError(t, conf.LoadConfig(filepath.Join("..", "etc", "config.yaml"), &cfg, conf.UseEnv()))
	require.Equal(t, "guild.v1", cfg.Name)
	require.False(t, cfg.Health)
	require.Equal(t, "cordis.guild.events.v1", cfg.Kafka.Topic)
	require.Equal(t, "cordis.guild.user.profiles.v1", cfg.Kafka.ProfileConsumerGroup)
	require.Empty(t, cfg.Kafka.Seeds)
	require.NotEmpty(t, cfg.Database.DataSource)
	require.Equal(t, "test-cursor-secret-at-least-32-bytes!", cfg.Cursor.Secret)
	require.False(t, cfg.Services.User.Middlewares.Duration)
	require.Equal(t, 10, cfg.Limits.OwnedGuilds())
	require.Equal(t, 100, cfg.Limits.JoinedGuilds())
	require.Equal(t, 250, cfg.Limits.Roles())
	require.Equal(t, 500, cfg.Limits.Channels())
	require.Equal(t, 100, cfg.Limits.ActiveInvites())
	require.Equal(t, 100, cfg.Limits.Overwrites())
	require.Equal(t, 255, cfg.Idempotency.KeyLength())
	require.Equal(t, 24*time.Hour, cfg.Idempotency.CreateGuildTTL())
	require.Equal(t, 24*time.Hour, cfg.Idempotency.CreateGuildRoleTTL())
	require.Equal(t, 24*time.Hour, cfg.Idempotency.CreateGuildChannelTTL())
	require.Equal(t, 24*time.Hour, cfg.Idempotency.CreateGuildInviteTTL())
}

func TestIdempotencyDefaults(t *testing.T) {
	cfg := IdempotencyConfig{}
	require.Equal(t, 255, cfg.KeyLength())
	require.Equal(t, 24*time.Hour, cfg.CreateGuildTTL())
	require.Equal(t, 24*time.Hour, cfg.CreateGuildRoleTTL())
	require.Equal(t, 24*time.Hour, cfg.CreateGuildChannelTTL())
	require.Equal(t, 24*time.Hour, cfg.CreateGuildInviteTTL())

	cfg = IdempotencyConfig{
		KeyMaxLength:                 64,
		CreateGuildTTLSeconds:        3600,
		CreateGuildRoleTTLSeconds:    7200,
		CreateGuildChannelTTLSeconds: 14400,
		CreateGuildInviteTTLSeconds:  28800,
	}
	require.Equal(t, 64, cfg.KeyLength())
	require.Equal(t, time.Hour, cfg.CreateGuildTTL())
	require.Equal(t, 2*time.Hour, cfg.CreateGuildRoleTTL())
	require.Equal(t, 4*time.Hour, cfg.CreateGuildChannelTTL())
	require.Equal(t, 8*time.Hour, cfg.CreateGuildInviteTTL())
}
