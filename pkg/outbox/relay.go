package outbox

import (
	"context"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/twmb/franz-go/pkg/kgo"
)

// RelayConfig controls the relay's behavior.
type RelayConfig struct {
	// BatchSize is the maximum number of events to claim per dispatch
	// cycle. Defaults to 100.
	BatchSize int

	// PollInterval is how long the dispatcher waits when there are no
	// pending events. Defaults to 50ms.
	PollInterval time.Duration

	// StaleThreshold is how long after locked_at an event is considered
	// stale and will be recovered back to pending. Defaults to 60s.
	StaleThreshold time.Duration

	// Retention is how long successfully-sent events are kept before
	// cleanup deletes them. Defaults to 1h.
	Retention time.Duration

	// CleanupBatch is the max rows to delete per cleanup cycle.
	// Defaults to 10000.
	CleanupBatch int

	// MaxRetries is the default max_retries for outbox events.
	// Defaults to 5.
	MaxRetries int
}

// DefaultRelayConfig returns a reasonable default configuration.
func DefaultRelayConfig() RelayConfig {
	return RelayConfig{
		BatchSize:      100,
		PollInterval:   50 * time.Millisecond,
		StaleThreshold: 60 * time.Second,
		Retention:      1 * time.Hour,
		CleanupBatch:   10000,
		MaxRetries:     5,
	}
}

// Relay polls the outbox table and publishes events to Kafka.
//
// The relay uses a single dispatcher goroutine that is woken by either
// a Notify() call (after a handler writes a new event) or a periodic
// ticker (to handle retries and edge cases). Because all events flow
// through one ORDER BY id path, ordering is naturally preserved and
// no per-entity version tracking is needed.
type Relay struct {
	cfg      RelayConfig
	DB       *sqlx.DB
	Producer Producer
	Logger   *slog.Logger

	// notifyCh wakes the dispatcher. Buffered with capacity 1;
	// sends are non-blocking — if the channel is full, the
	// dispatcher is already awake or will wake on the next tick.
	notifyCh chan struct{}

	ctx    context.Context
	cancel context.CancelFunc
}

// NewRelay creates a Relay. The caller must call Start() to begin.
func NewRelay(cfg RelayConfig, db *sqlx.DB, producer Producer, logger *slog.Logger) *Relay {
	if logger == nil {
		logger = slog.Default()
	}
	return &Relay{
		cfg:      cfg,
		DB:       db,
		Producer: producer,
		Logger:   logger,
		notifyCh: make(chan struct{}, 1),
	}
}

// Notify wakes the dispatcher. Safe for concurrent use. Handlers call
// this after committing a transaction that inserted outbox events.
// Safe to call on a nil Relay (e.g. in tests or when Kafka is disabled).
func (r *Relay) Notify() {
	if r == nil {
		return
	}
	select {
	case r.notifyCh <- struct{}{}:
	default:
		// Channel full — dispatcher is already awake or will
		// wake on the next tick.
	}
}

// Start launches the dispatcher and housekeeping goroutines.
func (r *Relay) Start(parent context.Context) {
	r.ctx, r.cancel = context.WithCancel(parent)
	go r.runDispatcher()
	go r.runHousekeeping()
}

// Stop cancels the relay's background goroutines. Safe to call on nil.
func (r *Relay) Stop() {
	if r == nil || r.cancel == nil {
		return
	}
	r.cancel()
}

func (r *Relay) runDispatcher() {
	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.notifyCh:
		case <-ticker.C:
		case <-r.ctx.Done():
			return
		}

		n := r.claimAndSend()

		// If we claimed a full batch there may be more pending
		// events — loop immediately without waiting.
		if n < r.cfg.BatchSize {
			select {
			case <-r.notifyCh:
			case <-ticker.C:
			case <-r.ctx.Done():
				return
			}
		}
	}
}

func (r *Relay) claimAndSend() int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	events, err := ClaimBatch(ctx, r.DB, Now(), r.cfg.BatchSize)
	if err != nil {
		if r.ctx.Err() == nil {
			r.Logger.Error("relay claim failed", "error", err)
		}
		return 0
	}
	if len(events) == 0 {
		return 0
	}

	// Enqueue all events asynchronously. Partitions are assigned by
	// key hash (franz-go StickyPartitioner default).
	for _, evt := range events {
		r.produceAsync(ctx, evt)
	}

	// Flush waits for all buffered records to reach the broker and
	// their promises to complete (MarkSent or Release).
	if err := r.Producer.Flush(ctx); err != nil {
		r.Logger.Error("relay flush failed", "error", err)
	}

	return len(events)
}

func (r *Relay) produceAsync(ctx context.Context, evt Event) {
	rec := &kgo.Record{
		Topic: evt.Topic,
		Key:   evt.Key,
		Value: evt.Payload,
	}

	// Promise is called serially by kgo after the broker responds.
	r.Producer.Produce(ctx, rec, func(_ *kgo.Record, err error) {
		if err != nil {
			r.Logger.Error("relay publish failed",
				"event_id", evt.ID,
				"topic", evt.Topic,
				"retry_count", evt.RetryCount,
				"error", err,
			)

			if evt.RetryCount+1 > r.cfg.MaxRetries {
				r.Logger.Error("relay retries exhausted, marking dead",
					"event_id", evt.ID,
					"retry_count", evt.RetryCount,
				)
			}

			if err := Release(context.Background(), r.DB, evt.ID); err != nil {
				r.Logger.Error("relay release failed",
					"event_id", evt.ID, "error", err,
				)
			}
			return
		}

		if err := MarkSent(context.Background(), r.DB, evt.ID, Now()); err != nil {
			r.Logger.Error("relay mark sent failed",
				"event_id", evt.ID, "error", err,
			)
		}
	})
}

func (r *Relay) runHousekeeping() {
	// Stale recovery: run every 30s.
	staleTicker := time.NewTicker(30 * time.Second)
	defer staleTicker.Stop()

	// Cleanup: run every 5min.
	cleanupTicker := time.NewTicker(5 * time.Minute)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-staleTicker.C:
			r.recoverStale()
		case <-cleanupTicker.C:
			r.cleanup()
		}
	}
}

func (r *Relay) recoverStale() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	staleBefore := time.Now().Add(-r.cfg.StaleThreshold).UnixMilli()
	if err := RecoverStale(ctx, r.DB, staleBefore); err != nil {
		if r.ctx.Err() == nil {
			r.Logger.Error("relay stale recovery failed", "error", err)
		}
	}
}

func (r *Relay) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	olderThan := time.Now().Add(-r.cfg.Retention).UnixMilli()
	n, err := Cleanup(ctx, r.DB, olderThan, r.cfg.CleanupBatch)
	if err != nil {
		if r.ctx.Err() == nil {
			r.Logger.Error("relay cleanup failed", "error", err)
		}
		return
	}
	if n > 0 {
		r.Logger.Info("relay cleaned up sent events", "count", n)
	}

	// Also drop dead events (retries exhausted).
	deadBefore := time.Now().Add(-1 * time.Hour).UnixMilli()
	if m, err := DropDead(ctx, r.DB, deadBefore); err != nil {
		r.Logger.Error("relay drop dead failed", "error", err)
	} else if m > 0 {
		r.Logger.Error("relay dropped dead events", "count", m)
	}
}
