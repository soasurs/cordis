package config

import (
	"path/filepath"
	"testing"
	"time"

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
	require.Equal(t, "127.0.0.1:6379", cfg.RateLimit.Redis.Host)
	require.True(t, cfg.RateLimit.Redis.NonBlock)
	require.Equal(t, 20000, int(cfg.RateLimit.Policies["source_ip_guard"].Limit))
	require.Equal(t, 300, int(cfg.RateLimit.Policies["authenticated_user"].Limit))
	require.Equal(t, time.Minute, cfg.RateLimit.Policies["source_ip_guard"].Window)
	require.False(t, cfg.Services.Authenticator.Middlewares.Duration)
	require.False(t, cfg.Services.User.Middlewares.Duration)
	require.False(t, cfg.Services.Message.Middlewares.Duration)
	require.False(t, cfg.Services.Guild.Middlewares.Duration)
}
