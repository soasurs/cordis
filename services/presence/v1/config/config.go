package config

import (
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/trace"
	"github.com/zeromicro/go-zero/zrpc"

	"github.com/soasurs/cordis/pkg/kafka"
)

type Config struct {
	Name        string
	ListenOn    string
	Timeout     int64 `json:",default=0"`
	Log         logx.LogConf
	DevServer   service.DevServerConfig `json:",optional"`
	Telemetry   trace.Config            `json:",optional"`
	Middlewares zrpc.ServerMiddlewaresConf
	Redis       redis.RedisConf
	Presence    PresenceConfig `json:",optional"`
	Kafka       KafkaConfig    `json:",optional"`
}

// RPCConfig builds the zrpc server configuration with the built-in health
// service disabled because pkg/probe owns grpc.health.v1.Health.
func (c Config) RPCConfig() zrpc.RpcServerConf {
	return zrpc.RpcServerConf{
		ServiceConf: service.ServiceConf{
			Name: c.Name, Log: c.Log, DevServer: c.DevServer, Telemetry: c.Telemetry,
		},
		ListenOn:    c.ListenOn,
		Timeout:     c.Timeout,
		Health:      false,
		Middlewares: c.Middlewares,
	}
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
	// UserSessionTTLSeconds controls how long a websocket session remains live
	// without another heartbeat from its gateway.
	UserSessionTTLSeconds int `json:",default=60"`
}

func (c PresenceConfig) UserSessionTTL() time.Duration {
	if c.UserSessionTTLSeconds <= 0 {
		return 60 * time.Second
	}
	return time.Duration(c.UserSessionTTLSeconds) * time.Second
}
