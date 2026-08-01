package partitionconsumer

import (
	"context"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/zeromicro/go-zero/core/logx"
)

const commitBatchSize = 32

type commitClient interface {
	CommitRecords(context.Context, ...*kgo.Record) error
}

// partitionCommitter coalesces completed offsets and commits them outside of
// partition workers. A stalled Kafka offset commit must not stall dispatching.
type partitionCommitter struct {
	client            commitClient
	ctx               context.Context
	cancel            context.CancelFunc
	interval          time.Duration
	timeout           time.Duration
	maxUncommitted    int
	onLagBackpressure func(partitionKey, bool)
	wake              chan struct{}
	done              chan struct{}
	stopOnce          sync.Once

	mu             sync.Mutex
	pendingOffsets map[partitionKey]*kgo.Record
	uncommitted    map[partitionKey]int
	backpressured  map[partitionKey]bool
	commitMu       sync.Mutex
}

func newPartitionCommitter(parent context.Context, client commitClient, interval, timeout time.Duration, maxUncommitted int, onLagBackpressure func(partitionKey, bool)) *partitionCommitter {
	ctx, cancel := context.WithCancel(parent)
	committer := &partitionCommitter{
		client:            client,
		ctx:               ctx,
		cancel:            cancel,
		interval:          interval,
		timeout:           timeout,
		maxUncommitted:    maxUncommitted,
		onLagBackpressure: onLagBackpressure,
		wake:              make(chan struct{}, 1),
		done:              make(chan struct{}),
		pendingOffsets:    make(map[partitionKey]*kgo.Record),
		uncommitted:       make(map[partitionKey]int),
		backpressured:     make(map[partitionKey]bool),
	}
	go committer.run()
	return committer
}

func (c *partitionCommitter) run() {
	defer close(c.done)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.flush()
		case <-c.wake:
			c.flush()
		}
	}
}

func (c *partitionCommitter) mark(record *kgo.Record) {
	if record == nil {
		return
	}
	key := partitionKey{topic: record.Topic, partition: record.Partition}
	completed := cloneCompletedRecord(record)
	c.mu.Lock()
	current := c.pendingOffsets[key]
	if current == nil || recordAfter(current, completed) {
		c.pendingOffsets[key] = completed
	}
	c.uncommitted[key]++
	backpressure := c.uncommitted[key] >= c.maxUncommitted && !c.backpressured[key]
	if backpressure {
		c.backpressured[key] = true
		// Keep the state transition and pause notification together. A fast
		// commit must not release the partition before the pause is applied.
		if c.onLagBackpressure != nil {
			c.onLagBackpressure(key, true)
		}
	}
	flush := len(c.pendingOffsets) >= commitBatchSize
	c.mu.Unlock()
	if flush {
		select {
		case c.wake <- struct{}{}:
		default:
		}
	}
}

func (c *partitionCommitter) flush() {
	c.commitMu.Lock()
	batch := c.takePending()
	if len(batch.records) == 0 || c.ctx.Err() != nil {
		c.commitMu.Unlock()
		return
	}
	commitCtx, cancel := context.WithTimeout(c.ctx, c.timeout)
	err := c.client.CommitRecords(commitCtx, batch.records...)
	cancel()
	if err == nil || c.ctx.Err() != nil {
		if err == nil {
			c.acknowledge(batch.counts)
		}
		c.commitMu.Unlock()
		return
	}
	logx.WithContext(c.ctx).Errorw("commit Kafka partition consumer offsets", logx.Field("error", err))
	c.requeue(batch.records)
	c.commitMu.Unlock()
}

type commitBatch struct {
	records []*kgo.Record
	counts  map[partitionKey]int
}

func (c *partitionCommitter) takePending() commitBatch {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.pendingOffsets) == 0 {
		return commitBatch{}
	}
	batch := commitBatch{
		records: make([]*kgo.Record, 0, len(c.pendingOffsets)),
		counts:  make(map[partitionKey]int, len(c.pendingOffsets)),
	}
	for key, record := range c.pendingOffsets {
		batch.records = append(batch.records, record)
		batch.counts[key] = c.uncommitted[key]
		delete(c.pendingOffsets, key)
	}
	return batch
}

func (c *partitionCommitter) acknowledge(counts map[partitionKey]int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, count := range counts {
		remaining := c.uncommitted[key] - count
		if remaining <= 0 {
			delete(c.uncommitted, key)
		} else {
			c.uncommitted[key] = remaining
		}
		if c.backpressured[key] && remaining < c.maxUncommitted {
			delete(c.backpressured, key)
			if c.onLagBackpressure != nil {
				c.onLagBackpressure(key, false)
			}
		}
	}
}

func (c *partitionCommitter) requeue(records []*kgo.Record) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, record := range records {
		key := partitionKey{topic: record.Topic, partition: record.Partition}
		current := c.pendingOffsets[key]
		if current == nil || recordAfter(current, record) {
			c.pendingOffsets[key] = record
		}
	}
}

func (c *partitionCommitter) dropPartitions(partitions map[string][]int32) {
	c.commitMu.Lock()
	defer c.commitMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	for topic, values := range partitions {
		for _, partition := range values {
			key := partitionKey{topic: topic, partition: partition}
			delete(c.pendingOffsets, key)
			delete(c.uncommitted, key)
			delete(c.backpressured, key)
		}
	}
}

func (c *partitionCommitter) commitRevoked(ctx context.Context, partitions map[string][]int32, completed []*kgo.Record) {
	c.commitMu.Lock()
	defer c.commitMu.Unlock()
	c.mu.Lock()
	for topic, values := range partitions {
		for _, partition := range values {
			key := partitionKey{topic: topic, partition: partition}
			delete(c.pendingOffsets, key)
			delete(c.uncommitted, key)
			delete(c.backpressured, key)
		}
	}
	c.mu.Unlock()
	if len(completed) == 0 {
		return
	}
	if err := c.client.CommitRecords(ctx, completed...); err != nil && ctx.Err() == nil {
		logx.WithContext(ctx).Errorw("commit revoked Kafka partition consumer offsets", logx.Field("error", err))
	}
}

func (c *partitionCommitter) stop() {
	c.stopOnce.Do(func() {
		c.cancel()
		<-c.done
	})
}

func cloneCompletedRecord(record *kgo.Record) *kgo.Record {
	if record == nil {
		return nil
	}
	return &kgo.Record{
		Topic:       record.Topic,
		Partition:   record.Partition,
		LeaderEpoch: record.LeaderEpoch,
		Offset:      record.Offset,
	}
}

func recordAfter(current, candidate *kgo.Record) bool {
	if current == nil || candidate == nil {
		return false
	}
	return kgo.EpochOffset{Epoch: current.LeaderEpoch, Offset: current.Offset}.
		Less(kgo.EpochOffset{Epoch: candidate.LeaderEpoch, Offset: candidate.Offset})
}
