package config

import (
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/trace"
	"github.com/zeromicro/go-zero/zrpc"

	"github.com/soasurs/cordis/pkg/probe"
	"github.com/soasurs/cordis/pkg/sessionregistry"
)

type Config struct {
	Name            string
	ProbeServer     probe.HTTPConfig
	Log             logx.LogConf
	Telemetry       trace.Config `json:",optional"`
	Kafka           KafkaConfig
	Redis           redis.RedisConf
	SessionRegistry sessionregistry.Config
	Dispatcher      DispatcherConfig
	Services        ServiceConfig
}

// ServiceConfig wires the User service, which resolves friend lists for
// presence fan-out.
type ServiceConfig struct {
	User zrpc.RpcClientConf
}

type KafkaConfig struct {
	Seeds         []string
	GuildTopic    string `json:",default=cordis.guild.events.v1"`
	MessageTopic  string `json:",default=cordis.message.events.v1"`
	UserTopic     string `json:",default=cordis.user.events.v1"`
	PresenceTopic string `json:",default=cordis.presence.events.v1"`
	ConsumerGroup string `json:",default=cordis.dispatcher.v1"`
}

type DispatcherConfig struct {
	DispatchTimeoutSeconds int `json:",default=5"`
	RetryMinMilliseconds   int `json:",default=100"`
	RetryMaxSeconds        int `json:",default=5"`
}
