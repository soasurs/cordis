package config

import (
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/trace"

	"github.com/soasurs/cordis/pkg/probe"
	"github.com/soasurs/cordis/pkg/sessionregistry"
)

type Config struct {
	Name            string
	ListenOn        string
	ProbeServer     probe.HTTPConfig
	Log             logx.LogConf
	Telemetry       trace.Config  `json:",optional"`
	Gateway         GatewayConfig `json:",optional"`
	RateLimit       RateLimitConfig
	Redis           redis.RedisConf
	SessionRegistry sessionregistry.Config
}

type RateLimitConfig struct {
	KeyPrefix             string        `json:",default=cordis:gateway:rate_limit:"`
	FallbackMaxKeys       int           `json:",default=10000"`
	FallbackRetryInterval time.Duration `json:",default=1s"`
	TrustedProxies        []string      `json:",optional"`
	Policies              map[string]RateLimitPolicy
}

type RateLimitPolicy struct {
	Limit  int64
	Window time.Duration
}

type GatewayConfig struct {
	WebSocketPath                    string `json:",default=/ws"`
	HeartbeatIntervalMs              int    `json:",default=45000"`
	IdentifyTimeoutSeconds           int    `json:",default=10"`
	MaxMessageBytes                  int64  `json:",default=65536"`
	MaxConnectionsPerInstance        int64  `json:",default=50000"`
	MaxPendingHandshakesPerInstance  int64  `json:",default=5000"`
	MaxPendingHandshakesPerIPv4Scope int64  `json:",default=100"`
	MaxPendingHandshakesPerIPv6Scope int64  `json:",default=20"`
	MaxClientEventsPerMinute         int    `json:",default=120"`
	HeartbeatTimeoutIntervals        int    `json:",default=2"`
	HeartbeatEarlyTolerancePercent   int    `json:",default=10"`
	CheckpointIntervalMs             int    `json:",default=5000"`
	CheckpointBatchSize              int    `json:",default=500"`
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

func (c GatewayConfig) HeartbeatTimeout() time.Duration {
	intervals := c.HeartbeatTimeoutIntervals
	if intervals <= 0 {
		intervals = 2
	}
	return c.HeartbeatInterval() * time.Duration(intervals)
}

// HeartbeatMinimumInterval returns the earliest accepted interval between
// client heartbeats, including the configured scheduling tolerance.
func (c GatewayConfig) HeartbeatMinimumInterval() time.Duration {
	tolerance := c.HeartbeatEarlyTolerancePercent
	if tolerance <= 0 {
		tolerance = 10
	}
	tolerance = min(tolerance, 50)
	interval := c.HeartbeatInterval()
	return interval - interval*time.Duration(tolerance)/100
}

func (c GatewayConfig) CheckpointInterval() time.Duration {
	if c.CheckpointIntervalMs <= 0 {
		return 5 * time.Second
	}
	return time.Duration(c.CheckpointIntervalMs) * time.Millisecond
}

func (c GatewayConfig) CheckpointLimit() int {
	if c.CheckpointBatchSize <= 0 {
		return 500
	}
	return c.CheckpointBatchSize
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

func (c GatewayConfig) ConnectionLimit() int64 {
	if c.MaxConnectionsPerInstance <= 0 {
		return 50000
	}
	return c.MaxConnectionsPerInstance
}

func (c GatewayConfig) PendingHandshakeLimit() int64 {
	if c.MaxPendingHandshakesPerInstance <= 0 {
		return 5000
	}
	return c.MaxPendingHandshakesPerInstance
}

func (c GatewayConfig) IPv4PendingHandshakeLimit() int64 {
	if c.MaxPendingHandshakesPerIPv4Scope <= 0 {
		return 100
	}
	return c.MaxPendingHandshakesPerIPv4Scope
}

func (c GatewayConfig) IPv6PendingHandshakeLimit() int64 {
	if c.MaxPendingHandshakesPerIPv6Scope <= 0 {
		return 20
	}
	return c.MaxPendingHandshakesPerIPv6Scope
}

func (c GatewayConfig) ClientEventLimit() int {
	if c.MaxClientEventsPerMinute <= 0 {
		return 120
	}
	return c.MaxClientEventsPerMinute
}
