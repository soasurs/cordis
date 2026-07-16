package config

import (
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/soasurs/cordis/pkg/sessionregistry"
)

type Config struct {
	Name            string
	Log             logx.LogConf
	Kafka           KafkaConfig
	Redis           redis.RedisConf
	SessionRegistry sessionregistry.Config
	Dispatcher      DispatcherConfig
}

type KafkaConfig struct {
	Seeds         []string
	GuildTopic    string `json:",default=cordis.guild.events.v1"`
	MessageTopic  string `json:",default=message.events"`
	ConsumerGroup string `json:",default=cordis.dispatcher.v1"`
}

type DispatcherConfig struct {
	DispatchTimeoutSeconds int `json:",default=5"`
	RetryMinMilliseconds   int `json:",default=100"`
	RetryMaxSeconds        int `json:",default=5"`
}
