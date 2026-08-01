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

// ServiceConfig wires audience lookups used for realtime fan-out.
type ServiceConfig struct {
	User    zrpc.RpcClientConf
	Guild   zrpc.RpcClientConf
	Message zrpc.RpcClientConf
}

type KafkaConfig struct {
	Seeds                 []string
	GuildTopic            string `json:",default=cordis.guild.events.v1"`
	MessageTopic          string `json:",default=cordis.message.events.v1"`
	UserTopic             string `json:",default=cordis.user.events.v1"`
	PresenceTopic         string `json:",default=cordis.presence.events.v1"`
	GuildConsumerGroup    string `json:",default=cordis.dispatcher.guild.v1"`
	MessageConsumerGroup  string `json:",default=cordis.dispatcher.message.v1"`
	UserConsumerGroup     string `json:",default=cordis.dispatcher.user.v1"`
	PresenceConsumerGroup string `json:",default=cordis.dispatcher.presence.v1"`
}

type DispatcherConfig struct {
	DispatchTimeoutSeconds     int `json:",default=5"`
	RetryMinMilliseconds       int `json:",default=100"`
	RetryMaxSeconds            int `json:",default=5"`
	MaxPollRecords             int `json:",default=32"`
	PartitionQueueSize         int `json:",default=16"`
	CommitIntervalMilliseconds int `json:",default=100"`
}
