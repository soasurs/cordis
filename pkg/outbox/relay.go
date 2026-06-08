package outbox

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/twmb/franz-go/pkg/kgo"
)

// RelayConfig controls the relay's polling behavior.
type RelayConfig struct {
	// NumWorkers is the number of concurrent poller goroutines.
	// Defaults to 4.
	NumWorkers int

	// BatchSize is the maximum number of events each worker claims per
	// poll. Defaults to 100.
	BatchSize int

	// PollInterval is how long a worker sleeps when no events are found.
	// Defaults to 100ms.
	PollInterval time.Duration

	// StaleThreshold is how long after locked_at an event is considered
	// stale and will be recovered back to pending. Defaults to 60s.
	StaleThreshold time.Duration

	// StaleInterval is how often the stale recovery goroutine runs.
	// Defaults to 30s.
	StaleInterval time.Duration

	// MaxRetries is the default max_retries for outbox events.
	// Defaults to 5.
	MaxRetries int
}

// DefaultRelayConfig returns a reasonable default configuration.
func DefaultRelayConfig() RelayConfig {
	return RelayConfig{
		NumWorkers:     4,
		BatchSize:      100,
		PollInterval:   100 * time.Millisecond,
		StaleThreshold: 60 * time.Second,
		StaleInterval:  30 * time.Second,
		MaxRetries:     5,
	}
}

// Relay polls the outbox table and publishes events to Kafka.
type Relay struct {
	cfg      RelayConfig
	DB       *sqlx.DB
	Producer Producer
	Logger   *slog.Logger

	// WaitGroup tracks in-flight ProduceSync + DB operations.
	// The caller should Wait() on this during graceful shutdown.
	Wg sync.WaitGroup

	ctx    context.Context
	cancel context.CancelFunc
}

// NewRelay creates a Relay. The caller must call Start() to begin polling.
func NewRelay(cfg RelayConfig, db *sqlx.DB, producer Producer, logger *slog.Logger) *Relay {
	return &Relay{
		cfg:      cfg,
		DB:       db,
		Producer: producer,
		Logger:   logger,
	}
}

// Start launches the poller workers and stale recovery goroutine.
// Call Stop() (or cancel the parent context) to shut down gracefully.
func (r *Relay) Start(parent context.Context) {
	r.ctx, r.cancel = context.WithCancel(parent)

	// Primary workers: claim pending events and publish to Kafka.
	for i := 0; i < r.cfg.NumWorkers; i++ {
		go r.runWorker(i)
	}

	// Stale recovery: periodically reset stuck events.
	go r.runStaleRecovery()
}

// Stop cancels the relay's context. In-flight operations (already-claimed
// events being published) will complete. Call Wg.Wait() after Stop() to
// wait for them, then close the Kafka client and database.
func (r *Relay) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *Relay) runWorker(_ int) {
	for {
		select {
		case <-r.ctx.Done():
			return
		default:
		}

		claimed := r.claimAndSend()
		if claimed == 0 {
			// Nothing pending — sleep briefly.
			select {
			case <-r.ctx.Done():
				return
			case <-time.After(r.cfg.PollInterval):
			}
		}
		// If we claimed a full batch, don't sleep — there may be more.
	}
}

func (r *Relay) claimAndSend() int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	events, err := ClaimBatch(ctx, r.DB, Now(), r.cfg.BatchSize)
	if err != nil {
		if r.ctx.Err() == nil {
			r.Logger.Error("outbox relay claim failed", "error", err)
		}
		return 0
	}
	if len(events) == 0 {
		return 0
	}

	for _, evt := range events {
		r.Wg.Add(1)
		r.publishOne(ctx, evt)
		r.Wg.Done()
	}

	return len(events)
}

func (r *Relay) publishOne(ctx context.Context, evt Event) {
	rec := &kgo.Record{
		Topic: evt.Topic,
		Key:   evt.Key,
		Value: evt.Payload,
	}

	results := r.Producer.ProduceSync(ctx, rec)
	if err := results.FirstErr(); err != nil {
		r.Logger.Error("outbox relay publish failed",
			"event_id", evt.ID,
			"topic", evt.Topic,
			"retry_count", evt.RetryCount,
			"error", err,
		)

		// If retries exhausted, leave it as dead so DropDead can clean
		// it later and an alert can fire.
		if evt.RetryCount+1 > r.cfg.MaxRetries {
			r.Logger.Error("outbox event retries exhausted, marking dead",
				"event_id", evt.ID,
				"retry_count", evt.RetryCount,
			)
		}

		if err := Release(ctx, r.DB, evt.ID); err != nil {
			r.Logger.Error("outbox relay release failed", "event_id", evt.ID, "error", err)
		}
		return
	}

	if err := Delete(ctx, r.DB, evt.ID); err != nil {
		r.Logger.Error("outbox relay delete failed", "event_id", evt.ID, "error", err)
		// The event was published, so the delete failure results in
		// at worst a duplicate (consumer must be idempotent). The
		// next stale recovery will release it and it will be
		// re-published.
	}
}

func (r *Relay) runStaleRecovery() {
	ticker := time.NewTicker(r.cfg.StaleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

		staleBefore := time.Now().Add(-r.cfg.StaleThreshold).UnixMilli()
		if err := RecoverStale(ctx, r.DB, staleBefore); err != nil {
			if r.ctx.Err() == nil {
				r.Logger.Error("outbox stale recovery failed", "error", err)
			}
		}

		deadBefore := time.Now().Add(-1 * time.Hour).UnixMilli()
		if n, err := DropDead(ctx, r.DB, deadBefore); err != nil {
			if r.ctx.Err() == nil {
				r.Logger.Error("outbox drop dead failed", "error", err)
			}
		} else if n > 0 {
			r.Logger.Error("outbox dropped dead events", "count", n)
		}

		cancel()
	}
}
