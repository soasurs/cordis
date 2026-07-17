package config

import (
	"time"

	"github.com/zeromicro/go-zero/zrpc"

	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/kafka"
)

type Config struct {
	zrpc.RpcServerConf
	Database database.Config
	Kafka    KafkaConfig `json:",optional"`
}

// KafkaConfig configures the optional relationship event stream.
type KafkaConfig struct {
	Seeds            []string `json:",optional"`
	Topic            string   `json:",default=cordis.user.events.v1"`
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
