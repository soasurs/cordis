package outbox

import (
	"context"
	"database/sql"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
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
// ticker (to handle retries and edge cases). Relay instances dynamically
// coordinate partition ownership with PostgreSQL advisory locks. A lock is
// held for one publish batch, then released so scaled replicas can compete.
// Consumers must still be idempotent by event_id because delivery is at least
// once across producer and process failures.
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

	mu      sync.Mutex
	started bool
	wg      sync.WaitGroup

	callbackWg sync.WaitGroup

	nextPartition atomic.Uint64
}

// NewRelay creates a Relay. The caller must call Start() to begin.
func NewRelay(cfg RelayConfig, db *sqlx.DB, producer Producer, logger *slog.Logger) *Relay {
	if logger == nil {
		logger = slog.Default()
	}
	relay := &Relay{
		cfg:      cfg,
		DB:       db,
		Producer: producer,
		Logger:   logger,
		notifyCh: make(chan struct{}, 1),
	}
	relay.nextPartition.Store(rand.Uint64() % PartitionCount)
	return relay
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return
	}
	r.ctx, r.cancel = context.WithCancel(parent)
	r.started = true
	r.wg.Add(2)
	go r.runDispatcher()
	go r.runHousekeeping()
}

// Stop stops new dispatch work and waits for active relay work to finish.
// Safe to call on nil.
func (r *Relay) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	cancel := r.cancel
	started := r.started
	r.mu.Unlock()
	if !started || cancel == nil {
		return
	}
	cancel()
	r.wg.Wait()
}

func (r *Relay) runDispatcher() {
	defer r.wg.Done()

	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.notifyCh:
		case <-ticker.C:
		case <-r.ctx.Done():
			return
		}

		if r.claimAndSend() > 0 {
			// Advisory locks are not fair. Yield briefly after every batch so
			// newly scaled replicas have a real chance to acquire a hot
			// partition before this replica scans again.
			timer := time.NewTimer(batchYieldDelay())
			select {
			case <-timer.C:
				r.Notify()
			case <-r.ctx.Done():
				timer.Stop()
				return
			}
		}
	}
}

func (r *Relay) claimAndSend() int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := r.DB.Conn(ctx)
	if err != nil {
		if r.ctx.Err() == nil {
			r.Logger.Error("relay database connection failed", "error", err)
		}
		return 0
	}
	defer conn.Close()

	partitions, err := ReadyPartitions(ctx, conn, Now(), PartitionCount)
	if err != nil {
		if r.ctx.Err() == nil {
			r.Logger.Error("relay ready partitions query failed", "error", err)
		}
		return 0
	}

	start := int(r.nextPartition.Add(1)-1) % PartitionCount
	for _, partition := range rotatedPartitions(partitions, start) {
		n, err := r.claimPartitionAndSend(ctx, conn, partition)
		if err != nil {
			if r.ctx.Err() == nil {
				r.Logger.Error("relay partition dispatch failed",
					"partition", partition,
					"error", err,
				)
			}
			continue
		}
		if n > 0 {
			r.nextPartition.Store(uint64((partition + 1) % PartitionCount))
			return n
		}
	}
	return 0
}

func (r *Relay) claimPartitionAndSend(ctx context.Context, conn *sql.Conn, partition int) (int, error) {
	locked, err := tryPartitionLock(ctx, conn, partition)
	if err != nil || !locked {
		return 0, err
	}
	defer r.unlockPartition(conn, partition)

	events, err := ClaimBatch(ctx, conn, Now(), r.cfg.BatchSize, partition)
	if err != nil {
		return 0, err
	}
	if len(events) == 0 {
		return 0, nil
	}

	// Enqueue all events asynchronously. The producer hashes non-nil keys,
	// so records for the same outbox key use the same Kafka partition.
	var remaining atomic.Int64
	remaining.Store(int64(len(events)))
	batchDone := make(chan struct{})
	onBatchDone := func() {
		if remaining.Add(-1) == 0 {
			close(batchDone)
		}
	}
	for _, evt := range events {
		r.produceAsync(ctx, conn, evt, onBatchDone)
	}

	// Flush waits for all buffered records to reach the broker and
	// their promises to complete (MarkSent or Release).
	flushErr := r.Producer.Flush(ctx)
	select {
	case <-batchDone:
	case <-ctx.Done():
		return len(events), ctx.Err()
	}
	if flushErr != nil {
		return len(events), flushErr
	}

	return len(events), nil
}

func (r *Relay) produceAsync(ctx context.Context, q Queryer, evt Event, batchDone func()) {
	rec := &kgo.Record{
		Topic: evt.Topic,
		Key:   evt.Key,
		Value: evt.Payload,
	}

	r.callbackWg.Add(1)
	// Promise is called serially by kgo after the broker responds.
	r.Producer.Produce(ctx, rec, func(_ *kgo.Record, err error) {
		defer r.callbackWg.Done()
		defer batchDone()

		if err != nil {
			r.Logger.Error("relay publish failed",
				"event_id", evt.ID,
				"topic", evt.Topic,
				"retry_count", evt.RetryCount,
				"error", err,
			)

			now := Now()
			if evt.RetryCount+1 > evt.MaxRetries {
				r.Logger.Error("relay retries exhausted, marking dead",
					"event_id", evt.ID,
					"attempts", evt.RetryCount+1,
				)
			}

			dbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			nextAttemptAt := now + retryDelay(evt.RetryCount).Milliseconds()
			if err := Release(dbCtx, q, evt.ID, now, nextAttemptAt); err != nil {
				r.Logger.Error("relay release failed",
					"event_id", evt.ID, "error", err,
				)
			}
			return
		}

		dbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := MarkSent(dbCtx, q, evt.ID, Now()); err != nil {
			r.Logger.Error("relay mark sent failed",
				"event_id", evt.ID, "error", err,
			)
		}
	})
}

// WaitCallbacks waits for all Kafka delivery promises and their database
// state transitions to finish. Call this after closing the producer.
func (r *Relay) WaitCallbacks() {
	if r == nil {
		return
	}
	r.callbackWg.Wait()
}

func retryDelay(retryCount int) time.Duration {
	const maxShift = 6
	shift := min(retryCount, maxShift)
	return time.Second * time.Duration(1<<shift)
}

func batchYieldDelay() time.Duration {
	return time.Duration(5+rand.IntN(16)) * time.Millisecond
}

func rotatedPartitions(partitions []int, start int) []int {
	if len(partitions) < 2 {
		return partitions
	}
	index := 0
	for index < len(partitions) && partitions[index] < start {
		index++
	}
	rotated := make([]int, 0, len(partitions))
	rotated = append(rotated, partitions[index:]...)
	rotated = append(rotated, partitions[:index]...)
	return rotated
}

func (r *Relay) runHousekeeping() {
	defer r.wg.Done()

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
	for partition := range PartitionCount {
		conn, err := r.DB.Conn(ctx)
		if err != nil {
			return
		}
		locked, err := tryPartitionLock(ctx, conn, partition)
		if err == nil && locked {
			err = RecoverStale(ctx, conn, staleBefore, partition)
			r.unlockPartition(conn, partition)
		}
		_ = conn.Close()
		if err != nil && r.ctx.Err() == nil {
			r.Logger.Error("relay stale recovery failed",
				"partition", partition,
				"error", err,
			)
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
}

func tryPartitionLock(ctx context.Context, conn *sql.Conn, partition int) (bool, error) {
	var locked bool
	err := conn.QueryRowContext(
		ctx,
		`SELECT pg_try_advisory_lock(
			hashtext(current_database() || ':' || current_schema() || ':outbox_messages'),
			$1
		)`,
		partition,
	).Scan(&locked)
	return locked, err
}

func (r *Relay) unlockPartition(conn *sql.Conn, partition int) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var unlocked bool
	if err := conn.QueryRowContext(
		ctx,
		`SELECT pg_advisory_unlock(
			hashtext(current_database() || ':' || current_schema() || ':outbox_messages'),
			$1
		)`,
		partition,
	).Scan(&unlocked); err != nil || !unlocked {
		r.Logger.Error("relay partition unlock failed",
			"partition", partition,
			"unlocked", unlocked,
			"error", err,
		)
	}
}
