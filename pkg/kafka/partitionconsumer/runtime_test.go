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

func TestPartitionRuntimeRetryHonorsRetryBudget(t *testing.T) {
	var attempts int
	runtime := newPartitionRuntime(Config{
		RetryMin:                time.Millisecond,
		RetryMax:                time.Millisecond,
		RetryMaxAttempts:        2,
		DropAfterRetryExhausted: true,
	}, func(context.Context, *kgo.Record) (bool, error) {
		attempts++
		return true, errors.New("temporary failure")
	})

	retryable, err, exhausted := runtime.retryRecord(context.Background(), testOffsetRecord("events", 0, 1))

	require.True(t, retryable)
	require.EqualError(t, err, "temporary failure")
	require.True(t, exhausted)
	require.Equal(t, 2, attempts)
}

func TestPartitionRuntimeWorkersDoNotBlockOtherPartitions(t *testing.T) {
	firstStarted := make(chan struct{})
	secondDone := make(chan struct{})
	releaseFirst := make(chan struct{})
	var once sync.Once
	runtime := newPartitionRuntime(Config{
		QueueSize:     1,
		RevokeTimeout: time.Second,
		CommitTimeout: time.Second,
	}, func(_ context.Context, record *kgo.Record) (bool, error) {
		if record.Partition == 0 {
			once.Do(func() { close(firstStarted) })
			<-releaseFirst
			return false, nil
		}
		close(secondDone)
		return false, nil
	})
	worker0 := testWorker(runtime, partitionKey{topic: "events", partition: 0})
	worker1 := testWorker(runtime, partitionKey{topic: "events", partition: 1})
	runtime.workers[worker0.key] = worker0
	runtime.workers[worker1.key] = worker1

	go worker0.run()
	go worker1.run()
	runtime.enqueue(testOffsetRecord("events", 0, 1))
	runtime.enqueue(testOffsetRecord("events", 1, 1))

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first partition did not start")
	}
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("second partition was blocked by first partition")
	}

	close(releaseFirst)
	runtime.stop()
}

func testWorker(runtime *partitionRuntime, key partitionKey) *partitionWorker {
	ctx, cancel := context.WithCancel(runtime.ctx)
	return &partitionWorker{
		runtime: runtime,
		key:     key,
		records: make(chan *kgo.Record, runtime.queueSize),
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
		active:  true,
	}
}
