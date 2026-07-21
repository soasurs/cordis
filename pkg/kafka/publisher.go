package kafka

import (
	"context"

	"github.com/twmb/franz-go/pkg/kgo"
)

type syncProducer interface {
	ProduceSync(ctx context.Context, records ...*kgo.Record) kgo.ProduceResults
}

// Publisher synchronously publishes events to one Kafka topic.
type Publisher struct {
	producer syncProducer
	topic    string
}

// NewPublisher creates a topic-bound publisher that injects trace context.
func NewPublisher(producer *kgo.Client, topic string) *Publisher {
	return &Publisher{producer: producer, topic: topic}
}

// Publish injects the current trace context and waits for broker acknowledgement.
func (p *Publisher) Publish(ctx context.Context, key, payload []byte) error {
	record := &kgo.Record{
		Topic: p.topic,
		Key:   key,
		Value: payload,
	}
	InjectTraceContext(ctx, record)
	return p.producer.ProduceSync(ctx, record).FirstErr()
}
