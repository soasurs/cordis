package outbox

import (
	"context"
	"sync"

	"github.com/twmb/franz-go/pkg/kgo"
)

// ProducedRecord holds the arguments passed to FakeProducer.Produce.
type ProducedRecord struct {
	Topic string
	Key   []byte
	Value []byte
}

type pendingRecord struct {
	rec     *kgo.Record
	promise func(*kgo.Record, error)
}

// FakeProducer is a test double that implements Producer. By default every
// produce succeeds. Set Err to make all produces fail, or use FailTopics
// to fail only specific topics.
//
// Produce is async — records are buffered and promises are called on Flush,
// matching the real kgo.Client semantics.
type FakeProducer struct {
	mu         sync.Mutex
	records    []ProducedRecord
	pending    []pendingRecord
	Err        error            // if set, all produces fail with this
	FailTopics map[string]error // per-topic error overrides
}

func (f *FakeProducer) Produce(_ context.Context, r *kgo.Record, promise func(*kgo.Record, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.records = append(f.records, ProducedRecord{
		Topic: r.Topic,
		Key:   r.Key,
		Value: r.Value,
	})
	f.pending = append(f.pending, pendingRecord{rec: r, promise: promise})
}

func (f *FakeProducer) Flush(_ context.Context) error {
	f.mu.Lock()
	pending := f.pending
	f.pending = nil
	f.mu.Unlock()

	// Call all pending promises, matching kgo's serial calling order.
	for _, p := range pending {
		var err error
		if f.Err != nil {
			err = f.Err
		} else if f.FailTopics != nil {
			if topicErr, ok := f.FailTopics[p.rec.Topic]; ok {
				err = topicErr
			}
		}
		p.promise(p.rec, err)
	}

	return nil
}

// Records returns a copy of all produced records.
func (f *FakeProducer) Records() []ProducedRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ProducedRecord, len(f.records))
	copy(out, f.records)
	return out
}

// Reset clears recorded state.
func (f *FakeProducer) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = nil
	f.pending = nil
}
