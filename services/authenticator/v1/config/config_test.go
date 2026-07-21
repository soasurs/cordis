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
	if cfg.Health {
		t.Fatal("built-in gRPC health service should be disabled")
	}
	if cfg.Services.User.Middlewares.Duration {
		t.Fatal("user client duration middleware should be disabled")
	}
	if cfg.Password.MaxConcurrency != 4 {
		t.Fatalf("unexpected password max concurrency: %d", cfg.Password.MaxConcurrency)
	}
}
