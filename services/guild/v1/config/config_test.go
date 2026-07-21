package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/conf"
)

func TestLoadConfig(t *testing.T) {
	var cfg Config
	require.NoError(t, conf.LoadConfig(filepath.Join("..", "etc", "config.yaml"), &cfg, conf.UseEnv()))
	require.Equal(t, "guild.v1", cfg.Name)
	require.False(t, cfg.Health)
	require.Equal(t, "cordis.guild.events.v1", cfg.Kafka.Topic)
	require.Empty(t, cfg.Kafka.Seeds)
	require.NotEmpty(t, cfg.Database.DataSource)
	require.False(t, cfg.Services.User.Middlewares.Duration)
	require.Equal(t, 10, cfg.Limits.OwnedGuilds())
	require.Equal(t, 100, cfg.Limits.JoinedGuilds())
	require.Equal(t, 250, cfg.Limits.Roles())
	require.Equal(t, 500, cfg.Limits.Channels())
	require.Equal(t, 100, cfg.Limits.ActiveInvites())
	require.Equal(t, 100, cfg.Limits.Overwrites())
}
