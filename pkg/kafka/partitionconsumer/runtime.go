package partitionconsumer

import (
	"context"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/zeromicro/go-zero/core/logx"
)

type partitionKey struct {
	topic     string
	partition int32
}

type partitionRuntime struct {
	cfg           Config
	handler       Handler
	maxPoll       int
	queueSize     int
	ctx           context.Context
	cancel        context.CancelFunc
	client        *kgo.Client
	committer     *partitionCommitter
	workers       map[partitionKey]*partitionWorker
	queuedRecords map[partitionKey][]*kgo.Record
	pauseReasons  map[partitionKey]partitionPauseReason
	mu            sync.Mutex
	stopOnce      sync.Once
	stopped       bool
}

type partitionWorker struct {
	runtime  *partitionRuntime
	client   *kgo.Client
	key      partitionKey
	records  chan *kgo.Record
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	stopOnce sync.Once

	mu            sync.Mutex
	active        bool
	lastCompleted *kgo.Record
}

type partitionPauseReason uint8

const (
	pauseForQueue partitionPauseReason = 1 << iota
	pauseForRetry
	pauseForCommit
)

func newPartitionRuntime(cfg Config, handler Handler) *partitionRuntime {
	cfg = cfg.withDefaults()
	ctx, cancel := context.WithCancel(context.Background())
	return &partitionRuntime{
		cfg:           cfg,
		handler:       handler,
		maxPoll:       cfg.MaxPollRecords,
		queueSize:     cfg.QueueSize,
		ctx:           ctx,
		cancel:        cancel,
		workers:       make(map[partitionKey]*partitionWorker),
		queuedRecords: make(map[partitionKey][]*kgo.Record),
		pauseReasons:  make(map[partitionKey]partitionPauseReason),
	}
}

func (r *partitionRuntime) assign(client *kgo.Client, assignments map[string][]int32) {
	r.mu.Lock()
	if r.client == nil {
		r.client = client
	}
	if r.stopped {
		r.mu.Unlock()
		return
	}
	workers := make([]*partitionWorker, 0)
	newAssignments := make(map[string][]int32)
	for topic, partitions := range assignments {
		for _, partition := range partitions {
			key := partitionKey{topic: topic, partition: partition}
			if _, exists := r.workers[key]; exists {
				continue
			}
			workerCtx, cancel := context.WithCancel(r.ctx)
			worker := &partitionWorker{
				runtime: r,
				client:  client,
				key:     key,
				records: make(chan *kgo.Record, r.queueSize),
				ctx:     workerCtx,
				cancel:  cancel,
				done:    make(chan struct{}),
				active:  true,
			}
			r.workers[key] = worker
			workers = append(workers, worker)
			newAssignments[topic] = append(newAssignments[topic], partition)
		}
	}
	if len(newAssignments) > 0 {
		client.ResumeFetchPartitions(newAssignments)
	}
	r.mu.Unlock()

	for _, worker := range workers {
		go worker.run()
	}
}

func (r *partitionRuntime) enqueue(record *kgo.Record) {
	if record == nil {
		return
	}
	key := partitionKey{topic: record.Topic, partition: record.Partition}
	r.mu.Lock()
	defer r.mu.Unlock()
	worker := r.workers[key]
	if r.stopped || worker == nil {
		return
	}
	if pending := r.queuedRecords[key]; len(pending) > 0 {
		r.queuedRecords[key] = append(pending, record)
		r.pauseLocked(worker, pauseForQueue)
		return
	}
	select {
	case worker.records <- record:
		return
	default:
	}
	r.queuedRecords[key] = append(r.queuedRecords[key], record)
	r.pauseLocked(worker, pauseForQueue)
}

func (r *partitionRuntime) pause(worker *partitionWorker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.workers[worker.key] != worker || r.stopped {
		return
	}
	r.pauseLocked(worker, pauseForRetry)
}

func (r *partitionRuntime) pauseLocked(worker *partitionWorker, reason partitionPauseReason) {
	current := r.pauseReasons[worker.key]
	if current&reason != 0 {
		return
	}
	r.pauseReasons[worker.key] = current | reason
	if current != 0 {
		return
	}
	worker.client.PauseFetchPartitions(map[string][]int32{
		worker.key.topic: {worker.key.partition},
	})
}

func (r *partitionRuntime) resumeLocked(worker *partitionWorker, reason partitionPauseReason) {
	current := r.pauseReasons[worker.key]
	if current&reason == 0 {
		return
	}
	next := current &^ reason
	if next != 0 {
		r.pauseReasons[worker.key] = next
		return
	}
	delete(r.pauseReasons, worker.key)
	worker.client.ResumeFetchPartitions(map[string][]int32{
		worker.key.topic: {worker.key.partition},
	})
}

func (r *partitionRuntime) setCommitBackpressure(key partitionKey, blocked bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	worker := r.workers[key]
	if worker == nil || r.stopped {
		return
	}
	if blocked {
		r.pauseLocked(worker, pauseForCommit)
		return
	}
	r.resumeLocked(worker, pauseForCommit)
}

func (r *partitionRuntime) afterProcessed(worker *partitionWorker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.workers[worker.key] != worker || r.stopped {
		return
	}
	for pending := r.queuedRecords[worker.key]; len(pending) > 0; {
		select {
		case worker.records <- pending[0]:
			pending = pending[1:]
			r.queuedRecords[worker.key] = pending
		default:
			return
		}
	}
	delete(r.queuedRecords, worker.key)
	if len(worker.records) == 0 {
		r.resumeLocked(worker, pauseForQueue|pauseForRetry)
	}
}

func (r *partitionRuntime) revoke(ctx context.Context, _ *kgo.Client, partitions map[string][]int32, commit bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	revokeCtx, cancel := context.WithTimeout(ctx, r.cfg.RevokeTimeout)
	defer cancel()
	workers := r.removeWorkers(partitions)
	r.stopWorkers(revokeCtx, workers)
	completed := make([]*kgo.Record, 0, len(workers))
	for _, worker := range workers {
		if record := worker.completed(); record != nil {
			completed = append(completed, record)
		}
	}
	if commit {
		r.committer.commitRevoked(revokeCtx, partitions, completed)
	} else {
		r.committer.dropPartitions(partitions)
	}
}

func (r *partitionRuntime) removeWorkers(partitions map[string][]int32) []*partitionWorker {
	r.mu.Lock()
	defer r.mu.Unlock()
	var workers []*partitionWorker
	for topic, values := range partitions {
		for _, partition := range values {
			key := partitionKey{topic: topic, partition: partition}
			worker := r.workers[key]
			if worker == nil {
				continue
			}
			delete(r.workers, key)
			delete(r.queuedRecords, key)
			delete(r.pauseReasons, key)
			workers = append(workers, worker)
		}
	}
	return workers
}

func (r *partitionRuntime) stopWorkers(parent context.Context, workers []*partitionWorker) {
	if len(workers) == 0 {
		return
	}
	if parent == nil {
		parent = context.Background()
	}
	stopCtx, cancel := context.WithTimeout(parent, r.cfg.RevokeTimeout)
	defer cancel()
	var wg sync.WaitGroup
	for _, worker := range workers {
		wg.Go(func() {
			if worker.stop(stopCtx) {
				return
			}
			logx.WithContext(stopCtx).Errorw("stop Kafka partition worker timed out",
				logx.Field("topic", worker.key.topic),
				logx.Field("partition", worker.key.partition),
				logx.Field("timeout", r.cfg.RevokeTimeout),
			)
		})
	}
	wg.Wait()
}

func (r *partitionRuntime) stop() {
	r.stopOnce.Do(func() {
		r.mu.Lock()
		r.stopped = true
		workers := make([]*partitionWorker, 0, len(r.workers))
		for key, worker := range r.workers {
			delete(r.workers, key)
			workers = append(workers, worker)
		}
		r.queuedRecords = make(map[partitionKey][]*kgo.Record)
		r.pauseReasons = make(map[partitionKey]partitionPauseReason)
		r.mu.Unlock()

		r.cancel()
		r.stopWorkers(context.Background(), workers)
		if r.committer != nil {
			r.committer.stop()
		}

		completed := make([]*kgo.Record, 0, len(workers))
		for _, worker := range workers {
			if record := worker.completed(); record != nil {
				completed = append(completed, record)
			}
		}
		if len(completed) == 0 || r.committer == nil {
			return
		}
		// lastCompleted is the lifecycle watermark. The async committer may have
		// already taken, requeued, or discarded its pending offset, so shutdown
		// commits the worker snapshots rather than relying on committer state.
		commitCtx, cancel := context.WithTimeout(context.Background(), r.cfg.CommitTimeout)
		defer cancel()
		if err := r.committer.client.CommitRecords(commitCtx, completed...); err != nil {
			logx.WithContext(commitCtx).Errorw("commit Kafka partition consumer shutdown offsets", logx.Field("error", err))
		}
	})
}

func (w *partitionWorker) run() {
	defer close(w.done)
	for {
		select {
		case <-w.ctx.Done():
			return
		case record := <-w.records:
			if !w.runtime.processRecord(w, record) {
				return
			}
		}
	}
}

func (r *partitionRuntime) processRecord(worker *partitionWorker, record *kgo.Record) bool {
	retryable, err := r.handler(worker.ctx, record)
	exhausted := false
	if err != nil && retryable {
		r.pause(worker)
		retryable, err, exhausted = r.retryRecord(worker.ctx, record)
	}
	if worker.ctx.Err() != nil {
		return false
	}
	if err != nil {
		if retryable && !exhausted {
			return false
		}
		message := "drop Kafka partition consumer record"
		if exhausted {
			message = "drop Kafka partition consumer record after retries"
		}
		logx.WithContext(worker.ctx).Errorw(message,
			logx.Field("topic", record.Topic),
			logx.Field("partition", record.Partition),
			logx.Field("offset", record.Offset),
			logx.Field("error", err),
		)
	}
	if !worker.markCompleted(record) {
		return false
	}
	worker.runtime.afterProcessed(worker)
	return true
}

func (r *partitionRuntime) retryRecord(ctx context.Context, record *kgo.Record) (bool, error, bool) {
	delay := r.cfg.RetryMin
	attempt := 1
	for ctx.Err() == nil {
		logx.WithContext(ctx).Errorw("retry Kafka partition consumer record",
			logx.Field("topic", record.Topic),
			logx.Field("partition", record.Partition),
			logx.Field("offset", record.Offset),
			logx.Field("attempt", attempt),
			logx.Field("retry_after", delay),
		)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return true, ctx.Err(), false
		case <-timer.C:
		}
		retryable, err := r.handler(ctx, record)
		if err == nil || !retryable {
			return retryable, err, false
		}
		if r.cfg.RetryMaxAttempts > 0 && attempt >= r.cfg.RetryMaxAttempts {
			if r.cfg.DropAfterRetryExhausted {
				return retryable, err, true
			}
			attempt = 0
		}
		attempt++
		delay = min(delay*2, r.cfg.RetryMax)
	}
	return true, ctx.Err(), false
}

func (w *partitionWorker) stop(ctx context.Context) bool {
	w.stopOnce.Do(func() {
		w.cancel()
		w.mu.Lock()
		w.active = false
		w.mu.Unlock()
	})
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-w.done:
		return true
	case <-ctx.Done():
		return false
	}
}

func (w *partitionWorker) markCompleted(record *kgo.Record) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.active {
		return false
	}
	// Keep the watermark and async commit hand-off under the same lock. Worker
	// shutdown takes this lock before marking the worker inactive, so lifecycle
	// code cannot snapshot an offset that was not fully handed off.
	w.lastCompleted = cloneCompletedRecord(record)
	if w.runtime.committer != nil {
		w.runtime.committer.mark(record)
	}
	return true
}

func (w *partitionWorker) completed() *kgo.Record {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastCompleted
}
