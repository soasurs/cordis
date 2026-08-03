package config

import (
	"time"

	"github.com/zeromicro/go-zero/zrpc"

	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/kafka/partitionconsumer"
)

type Config struct {
	zrpc.RpcServerConf
	ShutdownTimeout time.Duration   `json:",default=20s"`
	Database        database.Config `json:",optional"`
	Kafka           KafkaConfig     `json:",optional"`
	Cursor          CursorConfig
	ReadStates      ReadStatesConfig
	Limits          ResourceLimitsConfig
	Idempotency     IdempotencyConfig
	Outbox          OutboxConfig
	Services        ServiceConfig
}

// ShutdownDuration is the process shutdown budget, including the shared
// consumer's final worker stop and offset commit.
func (c Config) ShutdownDuration() time.Duration {
	if c.ShutdownTimeout <= 0 {
		return partitionconsumer.DefaultShutdownTimeout
	}
	return c.ShutdownTimeout
}

// CursorConfig holds the HMAC secret for opaque list continuation tokens.
type CursorConfig struct {
	Secret string
}

// ResourceLimitsConfig controls per-message collection limits.
type ResourceLimitsConfig struct {
	AttachmentsPerMessage int `json:",default=10"`
	MentionsPerMessage    int `json:",default=100"`
}

// Attachments returns the attachment limit per message.
func (c ResourceLimitsConfig) Attachments() int {
	if c.AttachmentsPerMessage <= 0 {
		return 10
	}
	return c.AttachmentsPerMessage
}

// Mentions returns the unique mentioned-user limit per message.
func (c ResourceLimitsConfig) Mentions() int {
	if c.MentionsPerMessage <= 0 {
		return 100
	}
	return c.MentionsPerMessage
}

// ReadStatesConfig controls read-state query batch size and aggregate concurrency.
type ReadStatesConfig struct {
	MaxConcurrentChannels int64 `json:",default=800"`
}

// IdempotencyConfig controls request-level idempotency retention and key
// validation for resource creation RPCs.
type IdempotencyConfig struct {
	KeyMaxLength            int `json:",default=255"`
	CreateMessageTTLSeconds int `json:",default=1800"`
}

// OutboxConfig controls transactional event outbox writes.
type OutboxConfig struct {
	MessageShardCount   int `json:",default=64"`
	ReadStateShardCount int `json:",default=64"`
}

// MessageShards returns the virtual shard count for message events.
func (c OutboxConfig) MessageShards() int {
	if c.MessageShardCount <= 0 {
		return 64
	}
	return c.MessageShardCount
}

// ReadStateShards returns the virtual shard count for read-state events.
func (c OutboxConfig) ReadStateShards() int {
	if c.ReadStateShardCount <= 0 {
		return 64
	}
	return c.ReadStateShardCount
}

// KeyLength returns the maximum accepted idempotency key length in bytes.
func (c IdempotencyConfig) KeyLength() int {
	if c.KeyMaxLength <= 0 {
		return 255
	}
	return c.KeyMaxLength
}

// CreateMessageTTL returns the retention period for CreateMessage idempotency
// keys.
func (c IdempotencyConfig) CreateMessageTTL() time.Duration {
	if c.CreateMessageTTLSeconds <= 0 {
		return 30 * time.Minute
	}
	return time.Duration(c.CreateMessageTTLSeconds) * time.Second
}

type ServiceConfig struct {
	Guild zrpc.RpcClientConf
	User  zrpc.RpcClientConf
	Media zrpc.RpcClientConf
}

// KafkaConfig controls the Kafka producer connection and the event topic
// used by this service. The whole section is optional.
type KafkaConfig struct {
	// Seeds is a list of bootstrap brokers, e.g. ["127.0.0.1:9092"].
	// Required when the Kafka section is present.
	Seeds []string

	// Topic is the Kafka topic for message events.
	Topic string `json:",default=cordis.message.events.v1"`

	// MentionsConsumerGroup is the consumer group of the mention expansion
	// worker, which consumes the message event topic.
	MentionsConsumerGroup string `json:",default=cordis.message.mentions.v1"`
}

// EventTopic returns the message event topic, falling back to the canonical
// topic when the optional Kafka section is absent so nested defaults do not
// apply.
func (c KafkaConfig) EventTopic() string {
	if c.Topic == "" {
		return "cordis.message.events.v1"
	}
	return c.Topic
}
