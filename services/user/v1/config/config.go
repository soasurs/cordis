package config

import (
	"github.com/zeromicro/go-zero/zrpc"

	"github.com/soasurs/cordis/pkg/database"
)

type Config struct {
	zrpc.RpcServerConf
	Database database.Config
	Kafka    KafkaConfig `json:",optional"`
	Cursor   CursorConfig
	Outbox   OutboxConfig
	Services ServiceConfig
}

// CursorConfig holds the HMAC secret for opaque list continuation tokens.
type CursorConfig struct {
	Secret string
}

type ServiceConfig struct {
	Media zrpc.RpcClientConf
}

// KafkaConfig configures the optional user event stream.
type KafkaConfig struct {
	Seeds []string `json:",optional"`
	Topic string   `json:",default=cordis.user.events.v1"`
}

// EventTopic returns the user event topic, falling back to the canonical
// topic when the optional Kafka section is absent so nested defaults do not
// apply.
func (c KafkaConfig) EventTopic() string {
	if c.Topic == "" {
		return "cordis.user.events.v1"
	}
	return c.Topic
}

// OutboxConfig controls transactional event outbox writes.
type OutboxConfig struct {
	ShardCount int `json:",default=64"`
}

// Shards returns the virtual shard count for user events.
func (c OutboxConfig) Shards() int {
	if c.ShardCount <= 0 {
		return 64
	}
	return c.ShardCount
}
