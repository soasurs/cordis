package config

import (
	"time"

	"github.com/zeromicro/go-zero/zrpc"

	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/kafka"
)

type Config struct {
	zrpc.RpcServerConf
	Database    database.Config `json:",optional"`
	Kafka       KafkaConfig     `json:",optional"`
	Cursor      CursorConfig
	ReadStates  ReadStatesConfig
	Limits      ResourceLimitsConfig
	Idempotency IdempotencyConfig
	Services    ServiceConfig
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
