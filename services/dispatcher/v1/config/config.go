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

const (
	DefaultDispatchTimeoutSeconds     = 5
	DefaultRetryMinMilliseconds       = 100
	DefaultRetryMaxSeconds            = 5
	DefaultMaxPollRecords             = 32
	DefaultPartitionQueueSize         = 16
	DefaultCommitIntervalMilliseconds = 100
	DefaultRevokeTimeoutSeconds       = 10
	DefaultMaxUncommittedRecords      = 128
)

type DispatcherConfig struct {
	DispatchTimeoutSeconds     int `json:",default=5"`
	RevokeTimeoutSeconds       int `json:",default=10"`
	RetryMinMilliseconds       int `json:",default=100"`
	RetryMaxSeconds            int `json:",default=5"`
	MaxPollRecords             int `json:",default=32"`
	PartitionQueueSize         int `json:",default=16"`
	CommitIntervalMilliseconds int `json:",default=100"`
	MaxUncommittedRecords      int `json:",default=128"`
}

func (c DispatcherConfig) WithDefaults() DispatcherConfig {
	if c.DispatchTimeoutSeconds <= 0 {
		c.DispatchTimeoutSeconds = DefaultDispatchTimeoutSeconds
	}
	if c.RevokeTimeoutSeconds <= 0 {
		c.RevokeTimeoutSeconds = DefaultRevokeTimeoutSeconds
	}
	if c.RetryMinMilliseconds <= 0 {
		c.RetryMinMilliseconds = DefaultRetryMinMilliseconds
	}
	if c.RetryMaxSeconds <= 0 {
		c.RetryMaxSeconds = DefaultRetryMaxSeconds
	}
	if c.MaxPollRecords <= 0 {
		c.MaxPollRecords = DefaultMaxPollRecords
	}
	if c.PartitionQueueSize <= 0 {
		c.PartitionQueueSize = DefaultPartitionQueueSize
	}
	if c.CommitIntervalMilliseconds <= 0 {
		c.CommitIntervalMilliseconds = DefaultCommitIntervalMilliseconds
	}
	if c.MaxUncommittedRecords <= 0 {
		c.MaxUncommittedRecords = DefaultMaxUncommittedRecords
	}
	return c
}
