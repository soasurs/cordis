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
	require.Equal(t, "presence.rpc", cfg.Name)
	require.False(t, cfg.RPCConfig().Health)
	require.Equal(t, 6064, cfg.DevServer.Port)
	require.True(t, cfg.DevServer.EnableMetrics)
	require.Equal(t, "127.0.0.1:6379", cfg.Redis.Host)
}
