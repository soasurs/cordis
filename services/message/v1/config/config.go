package config

import (
	"time"

	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/kafka"
	"github.com/soasurs/cordis/pkg/outbox"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Database database.Config `json:",optional"`
	Kafka    KafkaConfig     `json:",optional"`
	Outbox   OutboxConfig    `json:",optional"`
}

// KafkaConfig controls the Kafka producer connection and the event topic
// used by this service. The whole section is optional — when absent, the
// service runs without Kafka and outbox events accumulate until a relay is
// deployed.
type KafkaConfig struct {
	// Seeds is a list of bootstrap brokers, e.g. ["127.0.0.1:9092"].
	// Required when the Kafka section is present.
	Seeds []string

	// Topic is the Kafka topic for message events, e.g. "message.events".
	Topic string `json:",default=message.events"`
}

// ProducerConfig converts to the kafka package's config.
func (c KafkaConfig) ProducerConfig() kafka.ProducerConfig {
	return kafka.ProducerConfig{Seeds: c.Seeds}
}

// OutboxConfig controls the outbox relay (background dispatcher).
// All fields are optional — zero values use defaults from
// outbox.DefaultRelayConfig.
type OutboxConfig struct {
	BatchSize      int `json:",optional"`
	PollIntervalMs int `json:",optional"`
	StaleThreshold int `json:",optional"` // seconds
	RetentionMin   int `json:",optional"` // minutes, default 60
	CleanupBatch   int `json:",optional"`
	MaxRetries     int `json:",optional"`
}

// RelayConfig converts to the outbox package's RelayConfig, filling
// defaults from DefaultRelayConfig where values are unset.
func (c OutboxConfig) RelayConfig() outbox.RelayConfig {
	cfg := outbox.DefaultRelayConfig()
	if c.BatchSize > 0 {
		cfg.BatchSize = c.BatchSize
	}
	if c.PollIntervalMs > 0 {
		cfg.PollInterval = time.Duration(c.PollIntervalMs) * time.Millisecond
	}
	if c.StaleThreshold > 0 {
		cfg.StaleThreshold = time.Duration(c.StaleThreshold) * time.Second
	}
	if c.RetentionMin > 0 {
		cfg.Retention = time.Duration(c.RetentionMin) * time.Minute
	}
	if c.CleanupBatch > 0 {
		cfg.CleanupBatch = c.CleanupBatch
	}
	if c.MaxRetries > 0 {
		cfg.MaxRetries = c.MaxRetries
	}
	return cfg
}
