package kafka

import (
	"context"

	"github.com/twmb/franz-go/pkg/kgo"
)

type syncProducer interface {
	ProduceSync(ctx context.Context, records ...*kgo.Record) kgo.ProduceResults
}

// Record is one event to publish to a topic-bound Publisher.
type Record struct {
	Key     []byte
	Payload []byte
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
	return p.PublishBatch(ctx, []Record{{Key: key, Payload: payload}})
}

// PublishBatch injects the current trace context and waits for broker
// acknowledgement for all records in one producer call.
func (p *Publisher) PublishBatch(ctx context.Context, batch []Record) error {
	if len(batch) == 0 {
		return nil
	}
	records := make([]*kgo.Record, 0, len(batch))
	for _, item := range batch {
		record := &kgo.Record{
			Topic: p.topic,
			Key:   item.Key,
			Value: item.Payload,
		}
		InjectTraceContext(ctx, record)
		records = append(records, record)
	}
	return p.producer.ProduceSync(ctx, records...).FirstErr()
}
