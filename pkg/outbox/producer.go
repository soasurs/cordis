package outbox

import (
	"context"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Producer is the minimal Kafka producer interface the relay needs.
// Implementations must be safe for concurrent use.
type Producer interface {
	// Produce enqueues a record for asynchronous delivery. The promise
	// is called with the result when the broker responds (success or
	// failure). Records are keyed for hash-based partition assignment.
	Produce(ctx context.Context, r *kgo.Record, promise func(*kgo.Record, error))

	// Flush waits for all buffered records to be delivered and their
	// promises called.
	Flush(ctx context.Context) error
}

// FranzProducer wraps a *kgo.Client to satisfy the Producer interface.
type FranzProducer struct {
	Client *kgo.Client
}

func (p *FranzProducer) Produce(ctx context.Context, r *kgo.Record, promise func(*kgo.Record, error)) {
	p.Client.Produce(ctx, r, promise)
}

func (p *FranzProducer) Flush(ctx context.Context) error {
	return p.Client.Flush(ctx)
}
