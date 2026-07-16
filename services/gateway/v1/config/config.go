package config

import (
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

type Config struct {
	Name     string
	ListenOn string
	Log      logx.LogConf
	Gateway  GatewayConfig `json:",optional"`
	Redis    redis.RedisConf
}

type GatewayConfig struct {
	WebSocketPath          string `json:",default=/ws"`
	HeartbeatIntervalMs    int    `json:",default=45000"`
	IdentifyTimeoutSeconds int    `json:",default=10"`
	MaxMessageBytes        int64  `json:",default=65536"`
}

func (c GatewayConfig) WebSocketRoute() string {
	if c.WebSocketPath == "" {
		return "/ws"
	}
	return c.WebSocketPath
}

func (c GatewayConfig) HeartbeatInterval() time.Duration {
	if c.HeartbeatIntervalMs <= 0 {
		return 45 * time.Second
	}
	return time.Duration(c.HeartbeatIntervalMs) * time.Millisecond
}

func (c GatewayConfig) IdentifyTimeout() time.Duration {
	if c.IdentifyTimeoutSeconds <= 0 {
		return 10 * time.Second
	}
	return time.Duration(c.IdentifyTimeoutSeconds) * time.Second
}

func (c GatewayConfig) MessageLimit() int64 {
	if c.MaxMessageBytes <= 0 {
		return 65536
	}
	return c.MaxMessageBytes
}
