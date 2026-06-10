package config

import (
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Redis    redis.RedisConf
	Presence PresenceConfig `json:",optional"`
}

type PresenceConfig struct {
	// GatewayTTLSeconds controls how long a gateway heartbeat remains live.
	GatewayTTLSeconds int `json:",default=30"`
	// RouteTTLSeconds controls how long a channel route remains live without
	// another refresh from its owning gateway.
	RouteTTLSeconds int `json:",default=30"`
}

func (c PresenceConfig) GatewayTTL() time.Duration {
	if c.GatewayTTLSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.GatewayTTLSeconds) * time.Second
}

func (c PresenceConfig) RouteTTL() time.Duration {
	if c.RouteTTLSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.RouteTTLSeconds) * time.Second
}
