package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/conf"
)

func TestLoadConfig(t *testing.T) {
	var cfg Config
	err := conf.LoadConfig(filepath.Join("..", "etc", "config.yaml"), &cfg, conf.UseEnv())
	require.NoError(t, err)

	require.Equal(t, "error", cfg.Log.Level)
	require.False(t, cfg.Log.Stat)
	require.True(t, cfg.Observability.Metrics.Enabled)
	require.NotEmpty(t, cfg.Observability.Metrics.ListenOn)
	require.False(t, cfg.Services.Authenticator.Middlewares.Duration)
	require.False(t, cfg.Services.User.Middlewares.Duration)
}
