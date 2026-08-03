package kafka

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"
)

type syncProducer interface {
	ProduceSync(ctx context.Context, records ...*kgo.Record) kgo.ProduceResults
}

// Record is one event to publish to a topic-bound Publisher.
type Record struct {
	// ID optionally carries a caller correlation ID (for example the outbox
	// row ID) that is returned in the corresponding publish result.
	ID      int64
	Key     []byte
	Payload []byte
	// TraceContext carries serialized trace headers persisted by an outbox
	// writer. When present it is restored on the record; otherwise the current
	// context is injected.
	TraceContext []byte
}

// PublishResult is the per-record outcome of a batch publish. Results are
// returned in input order regardless of the order of kgo.ProduceResults.
type PublishResult struct {
	// ID is the correlation ID from the matching input Record.
	ID int64
	// Partition and Offset are populated only when Err is nil.
	Partition int32
	Offset    int64
	Err       error
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
	results := p.PublishBatchWithResults(ctx, batch)
	for _, result := range results {
		if result.Err != nil {
			return result.Err
		}
	}
	return nil
}

// PublishBatchWithResults injects the current trace context, waits for broker
// acknowledgement, and returns one result per input record in input order.
// A nil result slice is returned only for an empty batch.
func (p *Publisher) PublishBatchWithResults(ctx context.Context, batch []Record) []PublishResult {
	if len(batch) == 0 {
		return nil
	}
	records := make([]*kgo.Record, 0, len(batch))
	idsByRecord := make(map[*kgo.Record]int64, len(batch))
	for _, item := range batch {
		record := &kgo.Record{
			Topic: p.topic,
			Key:   item.Key,
			Value: item.Payload,
		}
		if len(item.TraceContext) > 0 {
			UnmarshalTraceContext(item.TraceContext, record)
		} else {
			InjectTraceContext(ctx, record)
		}
		records = append(records, record)
		idsByRecord[record] = item.ID
	}

	produceResults := p.producer.ProduceSync(ctx, records...)
	byRecord := make(map[*kgo.Record]kgo.ProduceResult, len(produceResults))
	for _, result := range produceResults {
		if result.Record != nil {
			byRecord[result.Record] = result
		}
	}

	results := make([]PublishResult, 0, len(records))
	for _, record := range records {
		result, ok := byRecord[record]
		if !ok {
			results = append(results, PublishResult{
				ID:  idsByRecord[record],
				Err: fmt.Errorf("kafka produce result missing for record %d", idsByRecord[record]),
			})
			continue
		}
		results = append(results, PublishResult{
			ID:        idsByRecord[record],
			Partition: record.Partition,
			Offset:    record.Offset,
			Err:       result.Err,
		})
	}
	return results
}
