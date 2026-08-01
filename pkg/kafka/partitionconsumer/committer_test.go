package partitionconsumer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

type recordingCommitClient struct {
	mu        sync.Mutex
	batches   [][]*kgo.Record
	err       error
	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
}

func (c *recordingCommitClient) CommitRecords(ctx context.Context, records ...*kgo.Record) error {
	if c.started != nil {
		c.startOnce.Do(func() { close(c.started) })
	}
	if c.release != nil {
		select {
		case <-c.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c.mu.Lock()
	c.batches = append(c.batches, cloneRecords(records))
	err := c.err
	c.mu.Unlock()
	return err
}

func (c *recordingCommitClient) committed() [][]*kgo.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneBatches(c.batches)
}

func TestPartitionCommitterCoalescesLatestOffsets(t *testing.T) {
	client := new(recordingCommitClient)
	committer := newPartitionCommitter(context.Background(), client, time.Hour, time.Second, 128, nil)
	defer committer.stop()

	committer.mark(testOffsetRecord("events", 0, 1))
	committer.mark(testOffsetRecord("events", 0, 3))
	committer.mark(testOffsetRecord("events", 1, 2))
	committer.flush()

	batches := client.committed()
	require.Len(t, batches, 1)
	require.Len(t, batches[0], 2)
	require.Equal(t, int64(3), offsetFor(batches[0], 0))
	require.Equal(t, int64(2), offsetFor(batches[0], 1))
}

func TestPartitionCommitterRequeuesFailedCommit(t *testing.T) {
	client := &recordingCommitClient{err: errors.New("commit failed")}
	committer := newPartitionCommitter(context.Background(), client, time.Hour, time.Second, 128, nil)
	defer committer.stop()

	committer.mark(testOffsetRecord("events", 0, 4))
	committer.flush()
	require.Len(t, committer.pendingOffsets, 1)

	client.mu.Lock()
	client.err = nil
	client.mu.Unlock()
	committer.flush()

	require.Empty(t, committer.pendingOffsets)
	require.Len(t, client.committed(), 2)
	require.Equal(t, int64(4), offsetFor(client.committed()[1], 0))
}

func TestPartitionCommitterDoesNotBlockMarksOnCommit(t *testing.T) {
	client := &recordingCommitClient{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	committer := newPartitionCommitter(context.Background(), client, time.Hour, time.Second, 128, nil)
	defer committer.stop()

	committer.mark(testOffsetRecord("events", 0, 1))
	flushDone := make(chan struct{})
	go func() {
		committer.flush()
		close(flushDone)
	}()
	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("commit did not start")
	}

	marked := make(chan struct{})
	go func() {
		committer.mark(testOffsetRecord("events", 0, 2))
		close(marked)
	}()
	select {
	case <-marked:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("mark was blocked by an in-flight commit")
	}
	close(client.release)
	select {
	case <-flushDone:
	case <-time.After(time.Second):
		t.Fatal("commit did not finish")
	}
}

func TestPartitionRuntimeLostDropsPendingOffsets(t *testing.T) {
	client := new(recordingCommitClient)
	committer := newPartitionCommitter(context.Background(), client, time.Hour, time.Second, 128, nil)
	defer committer.stop()

	key := partitionKey{topic: "events", partition: 0}
	worker := &partitionWorker{
		key:    key,
		active: true,
		cancel: func() {},
		done:   make(chan struct{}),
	}
	close(worker.done)
	runtime := &partitionRuntime{
		committer:     committer,
		workers:       map[partitionKey]*partitionWorker{key: worker},
		queuedRecords: make(map[partitionKey][]*kgo.Record),
		pauseReasons:  make(map[partitionKey]partitionPauseReason),
	}
	committer.mark(testOffsetRecord(key.topic, key.partition, 5))

	runtime.revoke(context.Background(), nil, map[string][]int32{"events": {0}}, false)
	committer.flush()

	require.Empty(t, client.committed())
}

func TestPartitionCommitterBackpressuresUncommittedPartitions(t *testing.T) {
	client := new(recordingCommitClient)
	changes := make(chan bool, 2)
	committer := newPartitionCommitter(
		context.Background(),
		client,
		time.Hour,
		time.Second,
		2,
		func(_ partitionKey, blocked bool) { changes <- blocked },
	)
	defer committer.stop()

	committer.mark(testOffsetRecord("events", 0, 1))
	committer.mark(testOffsetRecord("events", 0, 2))
	select {
	case blocked := <-changes:
		require.True(t, blocked)
	case <-time.After(time.Second):
		t.Fatal("partition was not backpressured")
	}

	committer.flush()
	select {
	case blocked := <-changes:
		require.False(t, blocked)
	case <-time.After(time.Second):
		t.Fatal("partition backpressure was not released")
	}
}

func TestPartitionWorkerStopIsBounded(t *testing.T) {
	workerCtx, cancel := context.WithCancel(context.Background())
	worker := &partitionWorker{
		ctx:    workerCtx,
		cancel: cancel,
		done:   make(chan struct{}),
		active: true,
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stopCancel()

	require.False(t, worker.stop(stopCtx))
	require.False(t, worker.active)

	close(worker.done)
	require.True(t, worker.stop(context.Background()))
}

func TestPartitionRuntimeStopContextBoundsShutdown(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	runtime := newPartitionRuntime(Config{
		RevokeTimeout: time.Second,
		CommitTimeout: time.Second,
	}, func(context.Context, *kgo.Record) (bool, error) {
		close(started)
		<-release
		return false, nil
	})
	client := new(recordingCommitClient)
	runtime.committer = newPartitionCommitter(runtime.ctx, client, time.Hour, time.Second, 128, nil)
	defer runtime.committer.stop()
	worker := testWorker(runtime, partitionKey{topic: "events", partition: 0})
	runtime.workers[worker.key] = worker
	go worker.run()
	runtime.enqueue(testOffsetRecord("events", 0, 1))

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("partition worker did not start")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := runtime.stopContext(stopCtx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Empty(t, client.committed())

	close(release)
	select {
	case <-worker.done:
	case <-time.After(time.Second):
		t.Fatal("partition worker did not exit after shutdown")
	}
}

func testOffsetRecord(topic string, partition int32, offset int64) *kgo.Record {
	return &kgo.Record{Topic: topic, Partition: partition, Offset: offset, LeaderEpoch: 1}
}

func offsetFor(records []*kgo.Record, partition int32) int64 {
	for _, record := range records {
		if record.Partition == partition {
			return record.Offset
		}
	}
	return -1
}

func cloneRecords(records []*kgo.Record) []*kgo.Record {
	cloned := make([]*kgo.Record, 0, len(records))
	for _, record := range records {
		cloned = append(cloned, cloneCompletedRecord(record))
	}
	return cloned
}

func cloneBatches(batches [][]*kgo.Record) [][]*kgo.Record {
	cloned := make([][]*kgo.Record, 0, len(batches))
	for _, batch := range batches {
		cloned = append(cloned, cloneRecords(batch))
	}
	return cloned
}
