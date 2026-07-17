package config

import (
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/zrpc"

	"github.com/soasurs/cordis/pkg/kafka"
)

type Config struct {
	zrpc.RpcServerConf
	Redis    redis.RedisConf
	Presence PresenceConfig `json:",optional"`
	Kafka    KafkaConfig    `json:",optional"`
}

// KafkaConfig configures the optional presence transition event stream.
type KafkaConfig struct {
	Seeds            []string `json:",optional"`
	Topic            string   `json:",default=cordis.presence.events.v1"`
	PublishTimeoutMs int      `json:",default=1000"`
}

func (c KafkaConfig) ProducerConfig() kafka.ProducerConfig {
	return kafka.ProducerConfig{
		Seeds:           c.Seeds,
		DeliveryTimeout: c.PublishTimeout(),
	}
}

func (c KafkaConfig) PublishTimeout() time.Duration {
	if c.PublishTimeoutMs <= 0 {
		return time.Second
	}
	return time.Duration(c.PublishTimeoutMs) * time.Millisecond
}

type PresenceConfig struct {
	// GatewayTTLSeconds controls how long a gateway heartbeat remains live.
	GatewayTTLSeconds int `json:",default=30"`
	// RouteTTLSeconds controls how long a channel route remains live without
	// another refresh from its owning gateway.
	RouteTTLSeconds int `json:",default=30"`
	// UserSessionTTLSeconds controls how long a websocket session remains live
	// without another heartbeat from its gateway.
	UserSessionTTLSeconds int `json:",default=60"`
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

func (c PresenceConfig) UserSessionTTL() time.Duration {
	if c.UserSessionTTLSeconds <= 0 {
		return 60 * time.Second
	}
	return time.Duration(c.UserSessionTTLSeconds) * time.Second
}
