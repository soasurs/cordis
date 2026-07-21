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
	require.Equal(t, "dispatcher.v1", cfg.Name)
	require.Equal(t, "error", cfg.Log.Level)
	require.Equal(t, 1.0, cfg.Telemetry.Sampler)
	require.Equal(t, "cordis.guild.events.v1", cfg.Kafka.GuildTopic)
	require.Equal(t, "cordis.message.events.v1", cfg.Kafka.MessageTopic)
	require.Equal(t, "127.0.0.1:6379", cfg.Redis.Host)
	require.Equal(t, []string{"127.0.0.1:2379"}, cfg.SessionRegistry.Hosts)
}
