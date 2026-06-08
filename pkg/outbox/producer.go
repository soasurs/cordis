package outbox

import (
	"context"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Producer is the minimal Kafka producer interface the relay needs.
// Implementations must be safe for concurrent use.
type Producer interface {
	ProduceSync(ctx context.Context, rs ...*kgo.Record) kgo.ProduceResults
}

// FranzProducer wraps a *kgo.Client to satisfy the Producer interface.
type FranzProducer struct {
	Client *kgo.Client
}

func (p *FranzProducer) ProduceSync(ctx context.Context, rs ...*kgo.Record) kgo.ProduceResults {
	return p.Client.ProduceSync(ctx, rs...)
}
