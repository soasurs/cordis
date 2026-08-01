// Package partitionconsumer provides a manually committed Kafka consumer with
// one serial worker and bounded buffering per assigned partition.
package partitionconsumer

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	DefaultMaxPollRecords        = 32
	DefaultQueueSize             = 16
	DefaultCommitInterval        = 100 * time.Millisecond
	DefaultMaxUncommittedRecords = 128
	DefaultRevokeTimeout         = 10 * time.Second
	DefaultCommitTimeout         = 5 * time.Second
	DefaultShutdownTimeout       = 20 * time.Second
	DefaultRetryMin              = 100 * time.Millisecond
	DefaultRetryMax              = 5 * time.Second
)

// Handler processes one Kafka record. It returns whether an error is
// retryable and the error itself. A non-retryable error is logged and the
// record is considered completed, which is useful for malformed or otherwise
// permanently invalid events.
type Handler func(context.Context, *kgo.Record) (retryable bool, err error)

// Config controls the partition workers and their offset coordinator.
// RetryMaxAttempts is the number of retries after the initial attempt. A zero
// value means retry indefinitely. When DropAfterRetryExhausted is false, a
// positive retry limit is also treated as indefinite retry after the limit is
// reached.
type Config struct {
	MaxPollRecords          int
	QueueSize               int
	CommitInterval          time.Duration
	MaxUncommittedRecords   int
	RevokeTimeout           time.Duration
	CommitTimeout           time.Duration
	ShutdownTimeout         time.Duration
	RetryMin                time.Duration
	RetryMax                time.Duration
	RetryMaxAttempts        int
	DropAfterRetryExhausted bool
}

func (c Config) withDefaults() Config {
	if c.MaxPollRecords <= 0 {
		c.MaxPollRecords = DefaultMaxPollRecords
	}
	if c.QueueSize <= 0 {
		c.QueueSize = DefaultQueueSize
	}
	if c.CommitInterval <= 0 {
		c.CommitInterval = DefaultCommitInterval
	}
	if c.MaxUncommittedRecords <= 0 {
		c.MaxUncommittedRecords = DefaultMaxUncommittedRecords
	}
	if c.RevokeTimeout <= 0 {
		c.RevokeTimeout = DefaultRevokeTimeout
	}
	if c.CommitTimeout <= 0 {
		c.CommitTimeout = DefaultCommitTimeout
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = DefaultShutdownTimeout
	}
	if c.RetryMin <= 0 {
		c.RetryMin = DefaultRetryMin
	}
	if c.RetryMax <= 0 {
		c.RetryMax = DefaultRetryMax
	}
	if c.RetryMax < c.RetryMin {
		c.RetryMax = c.RetryMin
	}
	return c
}

// Consumer is a partition-aware manually committed Kafka consumer.
type Consumer struct {
	client    *kgo.Client
	runtime   *partitionRuntime
	closed    chan struct{}
	closeOnce sync.Once
	closeErr  error
}

// New creates a consumer and appends the partition lifecycle callbacks to the
// supplied franz-go options. The returned consumer owns the Kafka client and
// must be closed by the caller.
func New(cfg Config, handler Handler, opts ...kgo.Opt) (*Consumer, error) {
	if handler == nil {
		return nil, errors.New("partition consumer handler is required")
	}
	cfg = cfg.withDefaults()
	runtime := newPartitionRuntime(cfg, handler)
	clientOpts := append([]kgo.Opt(nil), opts...)
	clientOpts = append(clientOpts,
		kgo.DisableAutoCommit(),
		kgo.OnPartitionsAssigned(func(_ context.Context, client *kgo.Client, assignments map[string][]int32) {
			runtime.assign(client, assignments)
		}),
		kgo.OnPartitionsRevoked(func(ctx context.Context, client *kgo.Client, partitions map[string][]int32) {
			runtime.revoke(ctx, client, partitions, true)
		}),
		kgo.OnPartitionsLost(func(_ context.Context, client *kgo.Client, partitions map[string][]int32) {
			runtime.revoke(context.Background(), client, partitions, false)
		}),
	)
	client, err := kgo.NewClient(clientOpts...)
	if err != nil {
		return nil, err
	}
	runtime.client = client
	runtime.committer = newPartitionCommitter(
		runtime.ctx,
		client,
		cfg.CommitInterval,
		cfg.CommitTimeout,
		cfg.MaxUncommittedRecords,
		func(key partitionKey, blocked bool) {
			runtime.setCommitBackpressure(key, blocked)
		},
	)
	return &Consumer{client: client, runtime: runtime, closed: make(chan struct{})}, nil
}

// Run polls records until ctx is cancelled or Close is called. It owns the
// runtime shutdown path and flushes the last completed watermark before
// returning, bounded by the configured shutdown timeout.
func (c *Consumer) Run(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), c.runtime.cfg.ShutdownTimeout)
		defer cancel()
		_ = c.runtime.stopContext(shutdownCtx)
	}()
	for {
		fetches := c.client.PollRecords(ctx, c.runtime.maxPoll)
		if ctx.Err() != nil || c.isClosed() {
			return
		}
		for _, fetchErr := range fetches.Errors() {
			logx.WithContext(ctx).Errorw("poll Kafka partition consumer record",
				logx.Field("topic", fetchErr.Topic),
				logx.Field("partition", fetchErr.Partition),
				logx.Field("error", fetchErr.Err),
			)
		}
		fetches.EachRecord(c.runtime.enqueue)
	}
}

// Close stops workers, synchronously flushes completed offsets, and closes the
// owned Kafka client. It is safe to call more than once and concurrently with
// Run. Close uses the runtime's configured shutdown budget.
func (c *Consumer) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), c.runtime.cfg.ShutdownTimeout)
	defer cancel()
	_ = c.CloseContext(ctx)
}

// CloseContext stops workers, flushes completed offsets, and closes the owned
// Kafka client using ctx as the overall shutdown deadline. It returns the
// shutdown or final-commit error, if any.
func (c *Consumer) CloseContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	c.closeOnce.Do(func() {
		c.closeErr = c.runtime.stopContext(ctx)
		close(c.closed)
		c.client.Close()
	})
	return c.closeErr
}

func (c *Consumer) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}
