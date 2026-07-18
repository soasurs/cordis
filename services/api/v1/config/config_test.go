package config

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/conf"

	apiratelimit "github.com/soasurs/cordis/services/api/v1/ratelimit"
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
	expectedPolicies := map[string]struct {
		limit  int64
		window time.Duration
	}{
		apiratelimit.PolicyRegisterIP:               {limit: 30, window: 10 * time.Minute},
		apiratelimit.PolicyRegisterEmail:            {limit: 3, window: time.Hour},
		apiratelimit.PolicyLoginIP:                  {limit: 600, window: 5 * time.Minute},
		apiratelimit.PolicyLoginEmail:               {limit: 20, window: 5 * time.Minute},
		apiratelimit.PolicyConfirmPasswordResetIP:   {limit: 30, window: 10 * time.Minute},
		apiratelimit.PolicyGetUserProfileIP:         {limit: 600, window: time.Minute},
		apiratelimit.PolicyCheckEmailAvailabilityIP: {limit: 60, window: time.Minute},
		apiratelimit.PolicyRecoveryRequestIP:        {limit: 30, window: 10 * time.Minute},
	}
	for name, expected := range expectedPolicies {
		policy, ok := cfg.RateLimit.Policies[name]
		require.True(t, ok, name)
		require.Equal(t, expected.limit, policy.Limit, name)
		require.Equal(t, expected.window, policy.Window, name)
	}
	require.False(t, cfg.Services.Authenticator.Middlewares.Duration)
	require.False(t, cfg.Services.User.Middlewares.Duration)
	require.False(t, cfg.Services.Message.Middlewares.Duration)
	require.False(t, cfg.Services.Guild.Middlewares.Duration)
}
