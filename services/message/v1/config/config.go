package config

import (
	"time"

	"github.com/zeromicro/go-zero/zrpc"

	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/kafka"
)

type Config struct {
	zrpc.RpcServerConf
	Database   database.Config `json:",optional"`
	Kafka      KafkaConfig     `json:",optional"`
	ReadStates ReadStatesConfig
	Services   ServiceConfig
}

// ReadStatesConfig controls bounded authorization fan-out and aggregate query
// concurrency for GetReadStates.
type ReadStatesConfig struct {
	AuthorizationConcurrency int   `json:",default=8"`
	MaxConcurrentChannels    int64 `json:",default=800"`
}

type ServiceConfig struct {
	Guild zrpc.RpcClientConf
	User  zrpc.RpcClientConf
}

// KafkaConfig controls the Kafka producer connection and the event topic
// used by this service. The whole section is optional.
type KafkaConfig struct {
	// Seeds is a list of bootstrap brokers, e.g. ["127.0.0.1:9092"].
	// Required when the Kafka section is present.
	Seeds []string

	// Topic is the Kafka topic for message events.
	Topic string `json:",default=cordis.message.events.v1"`

	// PublishTimeoutMs bounds how long a handler waits for a broker
	// acknowledgement. Publication failure does not fail the message RPC.
	PublishTimeoutMs int `json:",default=1000"`
}

// ProducerConfig converts to the kafka package's config.
func (c KafkaConfig) ProducerConfig() kafka.ProducerConfig {
	return kafka.ProducerConfig{
		Seeds:           c.Seeds,
		DeliveryTimeout: c.PublishTimeout(),
	}
}

// PublishTimeout returns the maximum time spent waiting for Kafka.
func (c KafkaConfig) PublishTimeout() time.Duration {
	if c.PublishTimeoutMs <= 0 {
		return time.Second
	}
	return time.Duration(c.PublishTimeoutMs) * time.Millisecond
}
