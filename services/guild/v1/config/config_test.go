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
	require.Equal(t, "guild.v1", cfg.Name)
	require.Equal(t, "cordis.guild.events.v1", cfg.Kafka.Topic)
	require.Empty(t, cfg.Kafka.Seeds)
	require.NotEmpty(t, cfg.Database.DataSource)
}
