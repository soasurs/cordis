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
	require.Equal(t, "cordis.guild.events.v1", cfg.Kafka.GuildTopic)
	require.Equal(t, "message.events", cfg.Kafka.MessageTopic)
	require.Equal(t, "127.0.0.1:6379", cfg.Redis.Host)
}
