package config

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/conf"

	"github.com/soasurs/cordis/pkg/clientip"
	apiratelimit "github.com/soasurs/cordis/services/api/v1/ratelimit"
)

func TestLoadConfig(t *testing.T) {
	var cfg Config
	err := conf.LoadConfig(filepath.Join("..", "etc", "config.yaml"), &cfg, conf.UseEnv())
	require.NoError(t, err)

	require.Equal(t, "error", cfg.Log.Level)
	require.False(t, cfg.Log.Stat)
	require.Equal(t, "0.0.0.0:6060", cfg.ProbeServer.ListenOn)
	require.Equal(t, 5*time.Second, cfg.Inbound.ReadTimeout)
	require.Equal(t, 5*time.Second, cfg.Inbound.ReadHeaderTimeout)
	require.Equal(t, 10*time.Second, cfg.Inbound.WriteTimeout)
	require.Equal(t, 2*time.Minute, cfg.Inbound.IdleTimeout)
	require.Equal(t, 15*time.Second, cfg.Inbound.ShutdownTimeout)
	require.Equal(t, 1048576, cfg.Inbound.MaxHeaderBytes)
	require.Equal(t, int64(2097152), cfg.Inbound.MaxRequestBytes)
	require.Equal(t, 1048576, cfg.Inbound.MaxMessageBytes)
	require.Equal(t, 3*time.Second, cfg.Inbound.Timeout)
	require.Equal(t, int64(10000), cfg.Inbound.MaxConcurrency)
	require.Equal(t, int64(900), cfg.Inbound.CPUThreshold)
	require.True(t, cfg.Inbound.Breaker)
	require.NoError(t, cfg.Inbound.Validate())
	require.Equal(t, "127.0.0.1:6379", cfg.RateLimit.Redis.Host)
	require.True(t, cfg.RateLimit.Redis.NonBlock)
	ipv4 := func(policy string) string { return apiratelimit.PolicyForFamily(policy, clientip.FamilyIPv4) }
	ipv6 := func(policy string) string { return apiratelimit.PolicyForFamily(policy, clientip.FamilyIPv6) }
	require.Equal(t, 20000, int(cfg.RateLimit.Policies[ipv4(apiratelimit.PolicySourceIPGuard)].Limit))
	require.Equal(t, 300, int(cfg.RateLimit.Policies["authenticated_user"].Limit))
	require.Equal(t, time.Minute, cfg.RateLimit.Policies[ipv6(apiratelimit.PolicySourceIPGuard)].Window)
	expectedPolicies := map[string]struct {
		limit  int64
		window time.Duration
	}{
		ipv4(apiratelimit.PolicyRegisterIP):                   {limit: 300, window: 10 * time.Minute},
		ipv6(apiratelimit.PolicyRegisterIP):                   {limit: 30, window: 10 * time.Minute},
		apiratelimit.PolicyRegisterEmail:                      {limit: 3, window: time.Hour},
		ipv4(apiratelimit.PolicyLoginIP):                      {limit: 1200, window: 5 * time.Minute},
		ipv6(apiratelimit.PolicyLoginIP):                      {limit: 600, window: 5 * time.Minute},
		apiratelimit.PolicyLoginEmail:                         {limit: 20, window: 5 * time.Minute},
		ipv4(apiratelimit.PolicyConfirmPasswordResetIP):       {limit: 300, window: 10 * time.Minute},
		ipv6(apiratelimit.PolicyConfirmPasswordResetIP):       {limit: 30, window: 10 * time.Minute},
		ipv4(apiratelimit.PolicyGetUserProfileIP):             {limit: 6000, window: time.Minute},
		ipv6(apiratelimit.PolicyGetUserProfileIP):             {limit: 600, window: time.Minute},
		ipv4(apiratelimit.PolicyCheckEmailAvailabilityIP):     {limit: 600, window: time.Minute},
		ipv6(apiratelimit.PolicyCheckEmailAvailabilityIP):     {limit: 60, window: time.Minute},
		ipv4(apiratelimit.PolicyCheckUsernameAvailabilityIP):  {limit: 600, window: time.Minute},
		ipv6(apiratelimit.PolicyCheckUsernameAvailabilityIP):  {limit: 60, window: time.Minute},
		apiratelimit.PolicyCheckUsernameAvailabilityUser:      {limit: 60, window: time.Minute},
		ipv4(apiratelimit.PolicyGetAvatarUploadConstraintsIP): {limit: 600, window: time.Minute},
		ipv6(apiratelimit.PolicyGetAvatarUploadConstraintsIP): {limit: 60, window: time.Minute},
		ipv4(apiratelimit.PolicyRecoveryRequestIP):            {limit: 300, window: 10 * time.Minute},
		ipv6(apiratelimit.PolicyRecoveryRequestIP):            {limit: 30, window: 10 * time.Minute},
		apiratelimit.PolicyCreateMessageUser:                  {limit: 30, window: 10 * time.Second},
		apiratelimit.PolicyCreateMessageChannel:               {limit: 120, window: 10 * time.Second},
		apiratelimit.PolicyRelationshipWrite:                  {limit: 60, window: time.Minute},
		apiratelimit.PolicySendFriendRequestMinute:            {limit: 10, window: time.Minute},
		apiratelimit.PolicySendFriendRequestDay:               {limit: 100, window: 24 * time.Hour},
		apiratelimit.PolicyBlockUnblockDebounce:               {limit: 1, window: 5 * time.Second},
		apiratelimit.PolicyCreateGuildUser:                    {limit: 5, window: time.Hour},
		apiratelimit.PolicyGuildResourceCreateActor:           {limit: 30, window: time.Minute},
		apiratelimit.PolicyGuildResourceCreateGuild:           {limit: 100, window: time.Hour},
		apiratelimit.PolicyJoinGuildInviteUser:                {limit: 10, window: 10 * time.Minute},
		ipv4(apiratelimit.PolicyJoinGuildInviteIP):            {limit: 300, window: 10 * time.Minute},
		ipv6(apiratelimit.PolicyJoinGuildInviteIP):            {limit: 30, window: 10 * time.Minute},
	}
	for name, expected := range expectedPolicies {
		policy, ok := cfg.RateLimit.Policies[name]
		require.True(t, ok, name)
		require.Equal(t, expected.limit, policy.Limit, name)
		require.Equal(t, expected.window, policy.Window, name)
	}
	require.Equal(t, int64(2), cfg.ReadStates.MaxConcurrencyPerUser)
	require.False(t, cfg.Services.Authenticator.Middlewares.Duration)
	require.False(t, cfg.Services.User.Middlewares.Duration)
	require.False(t, cfg.Services.Message.Middlewares.Duration)
	require.False(t, cfg.Services.Guild.Middlewares.Duration)
	require.Equal(t, int64(2000), cfg.Services.Authenticator.Timeout)
	require.Equal(t, int64(2000), cfg.Services.User.Timeout)
	require.Equal(t, int64(2000), cfg.Services.Message.Timeout)
	require.Equal(t, int64(2000), cfg.Services.Guild.Timeout)
}

func TestInboundConfigValidate(t *testing.T) {
	valid := InboundConfig{
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       time.Minute,
		ShutdownTimeout:   15 * time.Second,
		MaxHeaderBytes:    1,
		MaxRequestBytes:   2,
		MaxMessageBytes:   1,
		Timeout:           3 * time.Second,
		MaxConcurrency:    1,
		CPUThreshold:      900,
	}
	require.NoError(t, valid.Validate())

	invalid := valid
	invalid.ShutdownTimeout = invalid.WriteTimeout
	require.EqualError(t, invalid.Validate(), "inbound shutdown timeout must exceed write timeout")

	invalid = valid
	invalid.WriteTimeout = invalid.ReadTimeout + invalid.Timeout
	require.EqualError(t, invalid.Validate(), "inbound write timeout must exceed read timeout plus request timeout")

	invalid = valid
	invalid.MaxRequestBytes = 0
	require.EqualError(t, invalid.Validate(), "inbound max request bytes must be positive")
}
