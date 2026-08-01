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
	require.Equal(t, "0.0.0.0:6069", cfg.ProbeServer.ListenOn)
	require.Equal(t, "error", cfg.Log.Level)
	require.Equal(t, 1.0, cfg.Telemetry.Sampler)
	require.Equal(t, "cordis.guild.events.v1", cfg.Kafka.GuildTopic)
	require.Equal(t, "cordis.message.events.v1", cfg.Kafka.MessageTopic)
	require.Equal(t, "cordis.dispatcher.guild.v1", cfg.Kafka.GuildConsumerGroup)
	require.Equal(t, "cordis.dispatcher.message.v1", cfg.Kafka.MessageConsumerGroup)
	require.Equal(t, "cordis.dispatcher.user.v1", cfg.Kafka.UserConsumerGroup)
	require.Equal(t, "cordis.dispatcher.presence.v1", cfg.Kafka.PresenceConsumerGroup)
	require.Equal(t, 32, cfg.Dispatcher.MaxPollRecords)
	require.Equal(t, 16, cfg.Dispatcher.PartitionQueueSize)
	require.Equal(t, 100, cfg.Dispatcher.CommitIntervalMilliseconds)
	require.Equal(t, "127.0.0.1:6379", cfg.Redis.Host)
	require.Equal(t, []string{"127.0.0.1:2379"}, cfg.SessionRegistry.Hosts)
	require.Equal(t, []string{"127.0.0.1:3000"}, cfg.Services.User.Endpoints)
	require.Equal(t, []string{"127.0.0.1:3005"}, cfg.Services.Guild.Endpoints)
	require.Equal(t, []string{"127.0.0.1:3002"}, cfg.Services.Message.Endpoints)
}
