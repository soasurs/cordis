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
	require.Equal(t, "session.v1", cfg.Name)
	require.Equal(t, "0.0.0.0:3006", cfg.ListenOn)
	require.Equal(t, "session-local", cfg.Node.ID)
	require.Equal(t, 2048, cfg.Node.MaxReplayEvents)
	require.Equal(t, 500, cfg.Node.SubscribedChannelLimit())
	require.Equal(t, int64(20), cfg.RateLimit.Policies["identify_user"].Limit)
	require.Equal(t, 5*time.Minute, cfg.RateLimit.Policies["identify_user"].Window)
	require.Equal(t, "127.0.0.1:6379", cfg.Redis.Host)
	require.Equal(t, []string{"127.0.0.1:2379"}, cfg.SessionRegistry.Hosts)
	require.Equal(t, "/cordis/session/nodes", cfg.SessionRegistry.Prefix)
	require.Equal(t, []string{"127.0.0.1:3001"}, cfg.Services.Authenticator.Endpoints)
}
