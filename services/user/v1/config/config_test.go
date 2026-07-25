package config

import (
	"path/filepath"
	"testing"

	"github.com/zeromicro/go-zero/core/conf"
)

func TestLoadConfig(t *testing.T) {
	t.Setenv("CORDIS_CURSOR_SECRET", "test-cursor-secret-at-least-32-bytes!")
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
	if cfg.Cursor.Secret != "test-cursor-secret-at-least-32-bytes!" {
		t.Fatalf("unexpected cursor secret: %q", cfg.Cursor.Secret)
	}
}
