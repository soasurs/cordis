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
	require.Equal(t, "gateway.v1", cfg.Name)
	require.Equal(t, "0.0.0.0:8081", cfg.ListenOn)
	require.Equal(t, "info", cfg.Log.Level)
	require.Equal(t, 45000, cfg.Gateway.HeartbeatIntervalMs)
	require.Equal(t, "127.0.0.1:6379", cfg.Redis.Host)
	require.Equal(t, []string{"127.0.0.1:2379"}, cfg.SessionRegistry.Hosts)
}
