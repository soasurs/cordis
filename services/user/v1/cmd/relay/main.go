package main

import (
	"context"
	"flag"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/kafka"
	"github.com/soasurs/cordis/pkg/outbox/relay"
	"github.com/soasurs/cordis/services/user/v1/internal/eventoutbox"
)

var configPath = flag.String("c", "etc/relay.yaml", "config file of relay")

type Config struct {
	Name     string
	Database database.Config
	Kafka    KafkaConfig
	Outbox   OutboxConfig
	Relay    RelayConfig
}

type KafkaConfig struct {
	Seeds            []string
	Topic            string `json:",default=cordis.user.events.v1"`
	PublishTimeoutMs int    `json:",default=5000"`
}

func (c KafkaConfig) ProducerConfig() kafka.ProducerConfig {
	timeout := time.Duration(c.PublishTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return kafka.ProducerConfig{Seeds: c.Seeds, DeliveryTimeout: timeout}
}

type OutboxConfig struct {
	ShardCount int `json:",default=64"`
}

type RelayConfig struct {
	Workers        int  `json:",default=4"`
	BatchSize      int  `json:",default=100"`
	PollIntervalMs int  `json:",default=1000"`
	TimeSliceMs    int  `json:",default=100"`
	BackoffMinMs   int  `json:",default=100"`
	BackoffMaxMs   int  `json:",default=60000"`
	NotifyEnabled  bool `json:",default=true"`
}

func main() {
	flag.Parse()

	cfg := new(Config)
	if err := conf.LoadConfig(*configPath, cfg, conf.UseEnv()); err != nil {
		panic(err)
	}
	logx.MustSetup(logx.LogConf{ServiceName: cfg.Name, Level: "info"})

	db, err := database.NewPostgresPool(context.Background(), cfg.Database)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	producer, err := kafka.NewProducer(cfg.Kafka.ProducerConfig())
	if err != nil {
		panic(err)
	}
	defer producer.Close()
	publisher := kafka.NewPublisher(producer, cfg.Kafka.Topic)

	userRelay, err := relay.New(relay.Config{
		DB:            db,
		Tables:        eventoutbox.Tables(),
		Publisher:     publisher,
		Namespace:     eventoutbox.UserAdvisoryNamespace,
		NotifyChannel: notifyChannel(cfg.Relay.NotifyEnabled, eventoutbox.UserNotifyChannel),
		ListenerDSN:   cfg.Database.DataSource,
		Workers:       cfg.Relay.Workers,
		BatchSize:     cfg.Relay.BatchSize,
		PollInterval:  ms(cfg.Relay.PollIntervalMs, time.Second),
		TimeSlice:     ms(cfg.Relay.TimeSliceMs, 100*time.Millisecond),
		BackoffMin:    ms(cfg.Relay.BackoffMinMs, 100*time.Millisecond),
		BackoffMax:    ms(cfg.Relay.BackoffMaxMs, time.Minute),
	})
	if err != nil {
		panic(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var workers sync.WaitGroup
	workers.Go(func() {
		if err := userRelay.Run(ctx); err != nil && ctx.Err() == nil {
			logx.Errorw("user outbox relay stopped", logx.Field("error", err))
		}
	})

	logx.Infow("starting user outbox relay",
		logx.Field("workers", cfg.Relay.Workers),
		logx.Field("topic", cfg.Kafka.Topic),
	)
	<-ctx.Done()
	logx.Info("stopping user outbox relay")
	workers.Wait()
}

func notifyChannel(enabled bool, channel string) string {
	if !enabled {
		return ""
	}
	return channel
}

func ms(value int, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return time.Duration(value) * time.Millisecond
}
