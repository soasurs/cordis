// Package relay implements a domain-agnostic outbox relay.
//
// A relay scans virtual shards, takes a PostgreSQL session advisory lock per
// shard, publishes eligible head records to Kafka, and acknowledges them in
// one database transaction per batch. Multiple relay instances can run
// concurrently; the advisory lock guarantees that only one worker owns a
// shard at a time.
package relay

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/soasurs/cordis/pkg/kafka"
	"github.com/soasurs/cordis/pkg/outbox"
)

// Config configures one outbox relay.
type Config struct {
	// DB is the PostgreSQL handle that owns the outbox tables.
	DB *pgxpool.Pool
	// Tables identifies the stream and event tables.
	Tables outbox.Tables
	// Publisher is the topic-bound Kafka publisher.
	Publisher *kafka.Publisher
	// Namespace separates advisory locks and NOTIFY listeners between
	// relays that share a PostgreSQL instance.
	Namespace string
	// NotifyChannel is the PostgreSQL channel used for commit-after wakeups.
	// Empty disables LISTEN/NOTIFY and relies on polling only.
	NotifyChannel string
	// ListenerDSN is a PostgreSQL DSN used for the dedicated LISTEN
	// connection. It stays separate from the pool so WaitForNotification can
	// block on its own connection without occupying a pool slot.
	ListenerDSN string
	// Workers is the number of relay workers in this process.
	Workers int
	// BatchSize bounds records selected and published per shard iteration.
	BatchSize int
	// PollInterval is the fallback scan interval when no notification arrives.
	PollInterval time.Duration
	// TimeSlice bounds how long one worker keeps a shard before yielding.
	TimeSlice time.Duration
	// BackoffMin and BackoffMax bound the exponential retry delay.
	BackoffMin time.Duration
	BackoffMax time.Duration
}

// Metrics exposes lightweight counters for operational monitoring.
type Metrics struct {
	Published atomic.Int64
	Failed    atomic.Int64
	Deleted   atomic.Int64
}

// Relay runs workers that drain one outbox.
type Relay struct {
	cfg           Config
	namespaceHash int32
	wakeCh        chan struct{}
	metrics       Metrics
}

// New validates config and constructs a Relay.
func New(cfg Config) (*Relay, error) {
	if cfg.DB == nil {
		return nil, errors.New("relay: db is required")
	}
	if cfg.Tables.Streams == "" || cfg.Tables.Events == "" {
		return nil, errors.New("relay: stream and event table names are required")
	}
	if cfg.Publisher == nil {
		return nil, errors.New("relay: publisher is required")
	}
	if cfg.Namespace == "" {
		return nil, errors.New("relay: namespace is required")
	}
	if cfg.NotifyChannel != "" && cfg.ListenerDSN == "" {
		return nil, errors.New("relay: listener dsn is required when notify channel is set")
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	if cfg.TimeSlice <= 0 {
		cfg.TimeSlice = 100 * time.Millisecond
	}
	if cfg.BackoffMin <= 0 {
		cfg.BackoffMin = 100 * time.Millisecond
	}
	if cfg.BackoffMax <= 0 {
		cfg.BackoffMax = time.Minute
	}
	if cfg.BackoffMax < cfg.BackoffMin {
		return nil, errors.New("relay: backoff max must be >= backoff min")
	}
	return &Relay{
		cfg:           cfg,
		namespaceHash: hashNamespace(cfg.Namespace),
		wakeCh:        make(chan struct{}, 1),
	}, nil
}

// Run starts the worker pool and blocks until ctx is canceled.
func (r *Relay) Run(ctx context.Context) error {
	if r.cfg.NotifyChannel != "" {
		go r.listen(ctx)
	}

	var workers sync.WaitGroup
	for range r.cfg.Workers {
		workers.Go(func() {
			r.worker(ctx)
		})
	}
	workers.Wait()
	return nil
}

// Metrics returns the relay counters.
func (r *Relay) Metrics() *Metrics {
	return &r.metrics
}

func (r *Relay) worker(ctx context.Context) {
	for {
		if err := r.workOnce(ctx); err != nil && ctx.Err() == nil {
			logx.WithContext(ctx).Errorw("relay scan failed",
				logx.Field("namespace", r.cfg.Namespace),
				logx.Field("error", err),
			)
		}
		select {
		case <-ctx.Done():
			return
		case <-r.wakeCh:
		case <-time.After(r.cfg.PollInterval):
		}
	}
}

func (r *Relay) workOnce(ctx context.Context) error {
	conn, err := r.cfg.DB.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	shards, err := outbox.ListReadyShards(ctx, conn, r.cfg.Tables.Events, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	for _, shardID := range shards {
		acquired, err := r.tryLock(ctx, conn, shardID)
		if err != nil {
			return err
		}
		if !acquired {
			continue
		}
		more, processErr := r.processShard(ctx, conn, shardID)
		unlockErr := r.unlock(ctx, conn, shardID)
		if processErr != nil {
			return processErr
		}
		if unlockErr != nil {
			return unlockErr
		}
		if more {
			r.signalWake()
		}
	}
	return nil
}

// processShard drains a shard until SelectHeads returns empty or the time
// slice expires. The boolean reports whether the slice expired with possible
// remaining work so the caller can reschedule immediately.
func (r *Relay) processShard(ctx context.Context, conn *pgxpool.Conn, shardID int) (bool, error) {
	deadline := time.Now().Add(r.cfg.TimeSlice)
	for time.Now().Before(deadline) {
		heads, err := outbox.SelectHeads(ctx, conn, r.cfg.Tables.Events, shardID, time.Now().UnixMilli(), int64(r.cfg.BatchSize))
		if err != nil {
			return false, err
		}
		if len(heads) == 0 {
			return false, nil
		}

		records := make([]kafka.Record, 0, len(heads))
		for _, head := range heads {
			records = append(records, kafka.Record{
				ID:           head.OutboxID,
				Key:          head.Key,
				Payload:      head.Payload,
				TraceContext: head.TraceContext,
			})
		}
		results := r.cfg.Publisher.PublishBatchWithResults(ctx, records)

		var delivered []int64
		var failed []outbox.FailedUpdate
		now := time.Now().UnixMilli()
		for index, result := range results {
			if result.Err != nil {
				attempts := heads[index].Attempts + 1
				failed = append(failed, outbox.FailedUpdate{
					OutboxID:      heads[index].OutboxID,
					Attempts:      attempts,
					NextAttemptAt: now + r.backoff(attempts).Milliseconds(),
				})
				r.metrics.Failed.Add(1)
				continue
			}
			delivered = append(delivered, heads[index].OutboxID)
			r.metrics.Published.Add(1)
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return false, err
		}
		if err := outbox.DeleteDelivered(ctx, tx, r.cfg.Tables.Events, delivered); err != nil {
			_ = tx.Rollback(ctx)
			return false, err
		}
		if err := outbox.UpdateFailed(ctx, tx, r.cfg.Tables.Events, failed); err != nil {
			_ = tx.Rollback(ctx)
			return false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return false, err
		}
		r.metrics.Deleted.Add(int64(len(delivered)))
	}
	return true, nil
}

func (r *Relay) tryLock(ctx context.Context, conn *pgxpool.Conn, shardID int) (bool, error) {
	var acquired bool
	err := conn.QueryRow(
		ctx,
		"SELECT pg_try_advisory_lock($1, $2)",
		r.namespaceHash,
		int32(shardID),
	).Scan(&acquired)
	return acquired, err
}

func (r *Relay) unlock(ctx context.Context, conn *pgxpool.Conn, shardID int) error {
	_, err := conn.Exec(
		ctx,
		"SELECT pg_advisory_unlock($1, $2)",
		r.namespaceHash,
		int32(shardID),
	)
	return err
}

func (r *Relay) backoff(attempt int) time.Duration {
	delay := float64(r.cfg.BackoffMin) * math.Pow(2, float64(attempt-1))
	if delay > float64(r.cfg.BackoffMax) {
		delay = float64(r.cfg.BackoffMax)
	}
	jitter := 1.0 + (rand.Float64()*0.2 - 0.1)
	delay *= jitter
	if delay > float64(r.cfg.BackoffMax) {
		delay = float64(r.cfg.BackoffMax)
	}
	return time.Duration(delay)
}

func (r *Relay) listen(ctx context.Context) {
	delay := r.cfg.BackoffMin
	for {
		if err := r.listenOnce(ctx); err != nil && ctx.Err() == nil {
			logx.WithContext(ctx).Errorw("relay listener failed",
				logx.Field("namespace", r.cfg.Namespace),
				logx.Field("error", err),
			)
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = r.nextListenerDelay(delay)
	}
}

func (r *Relay) nextListenerDelay(current time.Duration) time.Duration {
	next := max(min(current*2, r.cfg.BackoffMax), r.cfg.BackoffMin)
	return next
}

func (r *Relay) listenOnce(ctx context.Context) error {
	if r.cfg.ListenerDSN == "" {
		return errors.New("relay: listener dsn is required for notify channel")
	}
	conn, err := pgx.Connect(ctx, r.cfg.ListenerDSN)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, "LISTEN "+quoteIdent(r.cfg.NotifyChannel)); err != nil {
		return err
	}
	for {
		_, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		r.signalWake()
	}
}

func (r *Relay) signalWake() {
	select {
	case r.wakeCh <- struct{}{}:
	default:
	}
}

func hashNamespace(namespace string) int32 {
	var value uint32
	for _, char := range namespace {
		value = value*31 + uint32(char)
	}
	return int32(value)
}

func quoteIdent(name string) string {
	if name == "" {
		panic("relay: empty identifier")
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
