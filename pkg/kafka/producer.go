// Package kafka provides shared Kafka producer initialization for all services.
//
// The producer is topic-agnostic — each [kgo.Record] carries its own Topic field.
// Services define their event topics in their own configuration and set them on
// each outbox event.
package kafka

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/zeromicro/go-zero/core/logx"
)

// ProducerConfig holds the configuration for creating a Kafka producer.
type ProducerConfig struct {
	// Seeds is a list of bootstrap broker addresses, e.g. ["127.0.0.1:9092"].
	Seeds []string
}

// NewProducer creates a new franz-go Kafka producer with sensible defaults:
// idempotent writes enabled (default), acks=all, unlimited retries.
// Callers must call Close() on the returned client during shutdown.
func NewProducer(cfg ProducerConfig) (*kgo.Client, error) {
	if len(cfg.Seeds) == 0 {
		return nil, fmt.Errorf("kafka seeds are required")
	}

	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Seeds...),
		// Keep the ordering contract explicit: records with the same key
		// always map to the same partition.
		kgo.RecordPartitioner(kgo.StickyKeyPartitioner(nil)),
		// Idempotent producer is enabled by default (acks=all, retries forever).
		// This gives us at-least-once within a single producer session.
	}

	cl, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("kafka new client: %w", err)
	}

	// Verify cluster reachability.
	if err := cl.Ping(context.Background()); err != nil {
		cl.Close()
		return nil, fmt.Errorf("kafka ping: %w", err)
	}

	logx.Infow("kafka producer connected",
		logx.Field("seeds", cfg.Seeds),
	)
	return cl, nil
}
