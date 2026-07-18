package config

import (
	"path/filepath"
	"testing"

	"github.com/zeromicro/go-zero/core/conf"
)

func TestLoadConfig(t *testing.T) {
	var cfg Config
	err := conf.LoadConfig(filepath.Join("..", "etc", "config.yaml"), &cfg, conf.UseEnv())
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.Log.Level != "error" || cfg.Log.Stat {
		t.Fatalf("unexpected log config: %+v", cfg.Log)
	}
	if cfg.Middlewares.Stat {
		t.Fatal("server stat middleware should be disabled")
	}
	if cfg.ListenOn == "" || cfg.Database.DataSource == "" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.Kafka.Topic != "cordis.message.events.v1" {
		t.Fatalf("unexpected Kafka topic: %q", cfg.Kafka.Topic)
	}
	if cfg.ReadStates.AuthorizationConcurrency != 8 {
		t.Fatalf("unexpected read states authorization concurrency: %d", cfg.ReadStates.AuthorizationConcurrency)
	}
	if cfg.ReadStates.MaxConcurrentChannels != 800 {
		t.Fatalf("unexpected read states max concurrent channels: %d", cfg.ReadStates.MaxConcurrentChannels)
	}
	if cfg.Limits.Attachments() != 10 || cfg.Limits.Mentions() != 100 {
		t.Fatalf("unexpected resource limits: %+v", cfg.Limits)
	}
}
