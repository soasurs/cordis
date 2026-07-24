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
	require.NoError(t, conf.LoadConfig(filepath.Join("..", "etc", "config.yaml"), &cfg, conf.UseEnv()))
	require.Equal(t, "gateway.v1", cfg.Name)
	require.Equal(t, "0.0.0.0:8081", cfg.ListenOn)
	require.Equal(t, "0.0.0.0:6065", cfg.ProbeServer.ListenOn)
	require.Equal(t, "info", cfg.Log.Level)
	require.Equal(t, 1.0, cfg.Telemetry.Sampler)
	require.Equal(t, "otlpgrpc", cfg.Telemetry.Batcher)
	require.Equal(t, "/", cfg.Gateway.WebSocketRoute())
	require.Equal(t, []string{"http://localhost:5173"}, cfg.Gateway.OriginPatterns)
	require.Equal(t, 45000, cfg.Gateway.HeartbeatIntervalMs)
	require.Equal(t, int64(1200), cfg.RateLimit.Policies["upgrade_ip_ipv4"].Limit)
	require.Equal(t, time.Minute, cfg.RateLimit.Policies["upgrade_ip_ipv4"].Window)
	require.Equal(t, int64(50000), cfg.Gateway.ConnectionLimit())
	require.Equal(t, int64(5000), cfg.Gateway.PendingHandshakeLimit())
	require.Equal(t, int64(100), cfg.Gateway.IPv4PendingHandshakeLimit())
	require.Equal(t, int64(20), cfg.Gateway.IPv6PendingHandshakeLimit())
	require.Equal(t, 120, cfg.Gateway.ClientEventLimit())
	require.Equal(t, 90*time.Second, cfg.Gateway.HeartbeatTimeout())
	require.Equal(t, 40500*time.Millisecond, cfg.Gateway.HeartbeatMinimumInterval())
	require.Equal(t, 5*time.Second, cfg.Gateway.CheckpointInterval())
	require.Equal(t, 500, cfg.Gateway.CheckpointLimit())
	require.Equal(t, "127.0.0.1:6379", cfg.Redis.Host)
	require.Equal(t, []string{"127.0.0.1:2379"}, cfg.SessionRegistry.Hosts)
}
