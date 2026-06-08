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

	if cfg.Observability.Log.Level != "error" {
		t.Fatalf("log level = %q, want error", cfg.Observability.Log.Level)
	}
	if !cfg.Observability.Metrics.Enabled || cfg.Observability.Metrics.ListenOn == "" {
		t.Fatalf("unexpected metrics config: %+v", cfg.Observability.Metrics)
	}
	if cfg.Services.Authenticator.Middlewares.Duration {
		t.Fatal("authenticator client duration middleware should be disabled")
	}
}
