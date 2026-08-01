package server

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	cordiskafka "github.com/soasurs/cordis/pkg/kafka"
	"github.com/soasurs/cordis/pkg/observability"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/dispatcher/v1/config"
	"github.com/soasurs/cordis/services/dispatcher/v1/internal/discovery"
)

const presenceDispatchConcurrency = 16

type eventEnvelope struct {
	Type           string          `json:"t"`
	Data           json.RawMessage `json:"d"`
	IdempotencyKey string          `json:"idempotency_key"`
}

type eventRouting struct {
	ID        eventID   `json:"id"`
	GuildID   eventID   `json:"guild_id"`
	ChannelID eventID   `json:"channel_id"`
	UserID    eventID   `json:"user_id"`
	OwnerID   eventID   `json:"owner_id"`
	GuildIDs  []eventID `json:"guild_ids"`
}

type eventID int64

func (id *eventID) UnmarshalJSON(value []byte) error {
	if len(value) == 0 || string(value) == "null" {
		*id = 0
		return nil
	}
	if value[0] == '"' {
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return err
		}
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return err
		}
		*id = eventID(parsed)
		return nil
	}
	parsed, err := strconv.ParseInt(string(value), 10, 64)
	if err != nil {
		return err
	}
	*id = eventID(parsed)
	return nil
}

type Server struct {
	cfg           config.Config
	consumers     []eventConsumer
	resolver      discovery.Resolver
	userClient    userv1.UserServiceClient
	guildClient   guildv1.GuildServiceClient
	messageClient messagev1.MessageServiceClient
	tracer        trace.Tracer

	mu      sync.Mutex
	clients map[string]sessionv1.SessionServiceClient
	conns   map[string]*grpc.ClientConn
}

type eventConsumer struct {
	client  *kgo.Client
	runtime *partitionRuntime
}

type partitionKey struct {
	topic     string
	partition int32
}

type partitionRuntime struct {
	server        *Server
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

const (
	commitBatchSize = 32
)

type partitionPauseReason uint8

const (
	pauseForQueue partitionPauseReason = 1 << iota
	pauseForRetry
	pauseForCommit
)

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

func newPartitionCommitter(
	parent context.Context,
	client commitClient,
	interval, timeout time.Duration,
	maxUncommitted int,
	onLagBackpressure func(partitionKey, bool),
) *partitionCommitter {
	if interval <= 0 {
		interval = commitInterval(0)
	}
	if timeout <= 0 {
		timeout = time.Duration(config.DefaultDispatchTimeoutSeconds) * time.Second
	}
	if maxUncommitted <= 0 {
		maxUncommitted = config.DefaultMaxUncommittedRecords
	}
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
	logx.WithContext(c.ctx).Errorw("commit dispatcher offsets", logx.Field("error", err))
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
		logx.WithContext(ctx).Errorw("commit revoked dispatcher offsets", logx.Field("error", err))
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

func newPartitionRuntime(server *Server, maxPoll, queueSize int) *partitionRuntime {
	if maxPoll <= 0 {
		maxPoll = config.DefaultMaxPollRecords
	}
	if queueSize <= 0 {
		queueSize = config.DefaultPartitionQueueSize
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &partitionRuntime{
		server:        server,
		maxPoll:       maxPoll,
		queueSize:     queueSize,
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
	revokeCtx, cancel := context.WithTimeout(ctx, r.revokeTimeout())
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

func (r *partitionRuntime) revokeTimeout() time.Duration {
	if r.server == nil {
		return time.Duration(config.DefaultRevokeTimeoutSeconds) * time.Second
	}
	return r.server.revokeTimeout()
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
	timeout := r.revokeTimeout()
	stopCtx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	var wg sync.WaitGroup
	for _, worker := range workers {
		wg.Go(func() {
			if worker.stop(stopCtx) {
				return
			}
			logx.WithContext(stopCtx).Errorw("stop dispatcher partition worker timed out",
				logx.Field("topic", worker.key.topic),
				logx.Field("partition", worker.key.partition),
				logx.Field("timeout", timeout),
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
		if len(completed) == 0 || r.client == nil {
			return
		}
		// lastCompleted is the lifecycle watermark. The async committer may have
		// already taken, requeued, or discarded its pending offset, so shutdown
		// commits the worker snapshots rather than relying on committer state.
		commitCtx, cancel := context.WithTimeout(context.Background(), r.server.dispatchTimeout())
		defer cancel()
		if err := r.client.CommitRecords(commitCtx, completed...); err != nil {
			logx.WithContext(commitCtx).Errorw("commit dispatcher shutdown offsets", logx.Field("error", err))
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
			if !w.runtime.server.processPartitionRecord(w, record) {
				return
			}
		}
	}
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

func New(
	cfg config.Config,
	resolver discovery.Resolver,
	userClient userv1.UserServiceClient,
	guildClient guildv1.GuildServiceClient,
	messageClient messagev1.MessageServiceClient,
) *Server {
	cfg.Dispatcher = cfg.Dispatcher.WithDefaults()
	if len(cfg.Kafka.Seeds) == 0 {
		panic("dispatcher kafka seeds are required")
	}
	specs := [][2]string{
		{
			defaultString(cfg.Kafka.GuildTopic, "cordis.guild.events.v1"),
			defaultString(cfg.Kafka.GuildConsumerGroup, "cordis.dispatcher.guild.v1"),
		},
		{
			defaultString(cfg.Kafka.MessageTopic, "cordis.message.events.v1"),
			defaultString(cfg.Kafka.MessageConsumerGroup, "cordis.dispatcher.message.v1"),
		},
		{
			defaultString(cfg.Kafka.UserTopic, "cordis.user.events.v1"),
			defaultString(cfg.Kafka.UserConsumerGroup, "cordis.dispatcher.user.v1"),
		},
		{
			defaultString(cfg.Kafka.PresenceTopic, "cordis.presence.events.v1"),
			defaultString(cfg.Kafka.PresenceConsumerGroup, "cordis.dispatcher.presence.v1"),
		},
	}
	seenTopics := make(map[string]struct{}, len(specs))
	seenGroups := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if _, exists := seenTopics[spec[0]]; exists {
			panic("dispatcher kafka topics must be unique")
		}
		if _, exists := seenGroups[spec[1]]; exists {
			panic("dispatcher kafka consumer groups must be unique")
		}
		seenTopics[spec[0]] = struct{}{}
		seenGroups[spec[1]] = struct{}{}
	}
	server := &Server{
		cfg: cfg, consumers: make([]eventConsumer, 0, len(specs)), resolver: resolver,
		userClient: userClient, guildClient: guildClient, messageClient: messageClient,
		clients: make(map[string]sessionv1.SessionServiceClient),
		conns:   make(map[string]*grpc.ClientConn),
		tracer:  otel.Tracer(observability.DispatcherInstrumentationName),
	}
	for _, spec := range specs {
		runtime := newPartitionRuntime(server, cfg.Dispatcher.MaxPollRecords, cfg.Dispatcher.PartitionQueueSize)
		consumer, err := kgo.NewClient(
			kgo.SeedBrokers(cfg.Kafka.Seeds...),
			kgo.ConsumerGroup(spec[1]),
			kgo.ConsumeTopics(spec[0]),
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
		if err != nil {
			for _, created := range server.consumers {
				created.runtime.stop()
				created.client.Close()
			}
			panic(err)
		}
		runtime.client = consumer
		runtime.committer = newPartitionCommitter(
			runtime.ctx,
			consumer,
			commitInterval(cfg.Dispatcher.CommitIntervalMilliseconds),
			server.dispatchTimeout(),
			cfg.Dispatcher.MaxUncommittedRecords,
			func(key partitionKey, blocked bool) {
				runtime.setCommitBackpressure(key, blocked)
			},
		)
		server.consumers = append(server.consumers, eventConsumer{client: consumer, runtime: runtime})
	}
	return server
}

func (s *Server) Run(ctx context.Context) {
	defer s.close()
	var wg sync.WaitGroup
	for _, consumer := range s.consumers {
		wg.Go(func() { s.runConsumer(ctx, consumer) })
	}
	wg.Wait()
}

func (s *Server) runConsumer(ctx context.Context, consumer eventConsumer) {
	defer consumer.runtime.stop()
	for {
		fetches := consumer.client.PollRecords(ctx, consumer.runtime.maxPoll)
		if ctx.Err() != nil {
			return
		}
		for _, fetchErr := range fetches.Errors() {
			logx.WithContext(ctx).Errorw("poll dispatcher event",
				logx.Field("topic", fetchErr.Topic),
				logx.Field("partition", fetchErr.Partition),
				logx.Field("error", fetchErr.Err),
			)
		}
		fetches.EachRecord(consumer.runtime.enqueue)
	}
}

func (s *Server) processPartitionRecord(worker *partitionWorker, record *kgo.Record) bool {
	permanent, err := s.dispatchRecord(worker.ctx, record)
	if err != nil && !permanent {
		worker.runtime.pause(worker)
		permanent, err = s.retryRecord(worker.ctx, record)
	}
	if worker.ctx.Err() != nil {
		return false
	}
	if err != nil {
		if !permanent {
			return false
		}
		logx.WithContext(worker.ctx).Errorw("drop invalid dispatcher event",
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

func (s *Server) retryRecord(ctx context.Context, record *kgo.Record) (bool, error) {
	delay := s.retryMin()
	for ctx.Err() == nil {
		logx.WithContext(ctx).Errorw("retry dispatcher event",
			logx.Field("topic", record.Topic),
			logx.Field("partition", record.Partition),
			logx.Field("offset", record.Offset),
			logx.Field("retry_after", delay),
		)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, ctx.Err()
		case <-timer.C:
		}
		permanent, err := s.dispatchRecord(ctx, record)
		if err == nil || permanent {
			return permanent, err
		}
		delay = min(delay*2, s.retryMax())
	}
	return false, ctx.Err()
}

func (s *Server) dispatchRecord(ctx context.Context, record *kgo.Record) (permanent bool, err error) {
	topic := ""
	partition := int32(0)
	if record != nil {
		topic = record.Topic
		partition = record.Partition
	}
	tracer := s.tracer
	if tracer == nil {
		tracer = otel.Tracer(observability.DispatcherInstrumentationName)
	}
	ctx, span := tracer.Start(
		cordiskafka.ExtractTraceContext(ctx, record),
		"process "+topic,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.destination.name", topic),
			attribute.String("messaging.consumer.group.name", s.consumerGroup(record)),
			attribute.Int64("messaging.destination.partition.id", int64(partition)),
			attribute.String("messaging.operation.name", "process"),
			attribute.String("messaging.operation.type", "process"),
		),
	)
	defer func() {
		result := "success"
		if err != nil {
			result = "retryable_failure"
			errorType := "dispatch"
			if permanent {
				result = "permanent_failure"
				errorType = "invalid_event"
			}
			span.SetAttributes(attribute.String("error.type", errorType))
			span.SetStatus(codes.Error, result)
		}
		span.SetAttributes(attribute.String("cordis.messaging.result", result))
		span.End()
	}()

	if record == nil {
		return true, errors.New("record is nil")
	}
	var event eventEnvelope
	if err := json.Unmarshal(record.Value, &event); err != nil {
		return true, err
	}
	if strings.TrimSpace(event.Type) == "" || !json.Valid(event.Data) {
		return true, errors.New("invalid event envelope")
	}
	if isCanonicalEventType(event.Type) {
		span.SetAttributes(attribute.String("cordis.event.type", event.Type))
	}
	var routing eventRouting
	if err := json.Unmarshal(event.Data, &routing); err != nil {
		return true, err
	}

	idempotencyKey, err := parseIdempotencyKey(event.IdempotencyKey)
	if err != nil {
		return true, err
	}

	switch event.Type {
	case realtime.EventMessageCreated, realtime.EventMessageUpdated, realtime.EventMessageDeleted,
		realtime.EventMessageReadUpdated:
		channelID := int64(routing.ChannelID)
		if channelID <= 0 {
			return true, errors.New("message event channel id is invalid")
		}
		guildID := int64(routing.GuildID)
		userID := int64(routing.UserID)
		switch {
		case guildID > 0 && userID == 0:
			return false, s.dispatchGuildMessage(ctx, guildID, channelID, event, idempotencyKey)
		case userID > 0 && guildID == 0:
			return false, s.dispatchUser(ctx, userID, event, idempotencyKey)
		case guildID == 0 && userID == 0:
			return true, errors.New("message event aggregate route is missing")
		default:
			return true, errors.New("message event aggregate route is invalid")
		}
	default:
		if event.Type == realtime.EventPresencePreferenceUpdated {
			userID := int64(routing.UserID)
			if userID <= 0 {
				return true, errors.New("presence preference event user id is invalid")
			}
			return false, s.dispatchUser(ctx, userID, event, idempotencyKey)
		}
		if event.Type == realtime.EventPresenceUpdated {
			userID := int64(routing.UserID)
			if userID <= 0 {
				return true, errors.New("presence event user id is invalid")
			}
			return false, s.dispatchPresence(ctx, userID, event, routing, idempotencyKey)
		}
		if strings.HasPrefix(event.Type, "presence.") {
			return true, errors.New("unsupported presence event type")
		}
		// relationship.* and dm.* records are user-routed: the payload
		// user_id names the recipient.
		if strings.HasPrefix(event.Type, "relationship.") || strings.HasPrefix(event.Type, "dm.") {
			userID := int64(routing.UserID)
			if userID <= 0 {
				return true, errors.New("user-routed event user id is invalid")
			}
			return false, s.dispatchUser(ctx, userID, event, idempotencyKey)
		}
		if event.Type == realtime.EventUserProfileUpdated {
			userID := int64(routing.UserID)
			if userID <= 0 {
				return true, errors.New("profile event user id is invalid")
			}
			return false, s.dispatchUserProfile(ctx, userID, event, idempotencyKey)
		}
		if !strings.HasPrefix(event.Type, "guild.") {
			return true, errors.New("unsupported event type")
		}
		guildID := int64(routing.GuildID)
		if guildID == 0 &&
			(event.Type == realtime.EventGuildCreated || event.Type == realtime.EventGuildUpdated || event.Type == realtime.EventGuildDeleted) {
			guildID = int64(routing.ID)
		}
		if guildID <= 0 {
			return true, errors.New("guild event guild id is invalid")
		}
		if event.Type == realtime.EventGuildCreated && routing.OwnerID <= 0 {
			return true, errors.New("guild created owner id is invalid")
		}
		if event.Type == realtime.EventGuildMemberJoined && routing.UserID <= 0 {
			return true, errors.New("guild member joined user id is invalid")
		}
		return false, s.dispatchGuild(ctx, guildID, event, routing, idempotencyKey)
	}
}

func parseIdempotencyKey(value string) (int64, error) {
	idempotencyKey, err := strconv.ParseInt(value, 10, 64)
	if err != nil || idempotencyKey <= 0 || strconv.FormatInt(idempotencyKey, 10) != value {
		return 0, errors.New("invalid idempotency key")
	}
	return idempotencyKey, nil
}

// dispatchGuildMessage uses the aggregate Guild route to locate candidate
// Session nodes. Each node filters recipients through its visibility snapshots.
func (s *Server) dispatchGuildMessage(ctx context.Context, guildID, channelID int64, event eventEnvelope, idempotencyKey int64) error {
	nodes, err := s.resolver.Resolve(ctx, discovery.RouteGuild, guildID)
	if err != nil {
		return err
	}
	return s.forEachNode(ctx, nodes, func(ctx context.Context, client sessionv1.SessionServiceClient) error {
		req := new(sessionv1.DispatchGuildMessageEventRequest)
		req.SetGuildId(guildID)
		req.SetChannelId(channelID)
		req.SetEvent(protoEvent(event, idempotencyKey))
		_, err := client.DispatchGuildMessageEvent(ctx, req)
		return err
	})
}

// dispatchUser fans a user-routed event out to the recipient's session
// nodes only.
func (s *Server) dispatchUser(ctx context.Context, userID int64, event eventEnvelope, idempotencyKey int64) error {
	nodes, err := s.resolver.Resolve(ctx, discovery.RouteUser, userID)
	if err != nil {
		return err
	}
	return s.forEachNode(ctx, nodes, func(ctx context.Context, client sessionv1.SessionServiceClient) error {
		req := new(sessionv1.DispatchUserEventRequest)
		req.SetUserId(userID)
		req.SetEvent(protoEvent(event, idempotencyKey))
		_, err := client.DispatchUserEvent(ctx, req)
		return err
	})
}

// dispatchPresence fans a presence transition out along two paths: the
// user's guilds (shared-guild members) and their friends plus their own
// other devices. A recipient reachable through both paths receives the
// event more than once; presence updates are idempotent state, so
// duplicates are harmless.
func (s *Server) dispatchPresence(ctx context.Context, userID int64, event eventEnvelope, routing eventRouting, idempotencyKey int64) error {
	friends, err := s.friendIDs(ctx, userID)
	if err != nil {
		return err
	}

	seenGuilds := make(map[int64]struct{}, len(routing.GuildIDs))
	guildIDs := make([]int64, 0, len(routing.GuildIDs))
	for _, rawGuildID := range routing.GuildIDs {
		guildID := int64(rawGuildID)
		if guildID <= 0 {
			continue
		}
		if _, ok := seenGuilds[guildID]; ok {
			continue
		}
		seenGuilds[guildID] = struct{}{}
		guildIDs = append(guildIDs, guildID)
	}

	seenRecipients := make(map[int64]struct{}, len(friends)+1)
	recipientIDs := make([]int64, 0, len(friends)+1)
	for _, friendID := range friends {
		if friendID <= 0 || friendID == userID {
			continue
		}
		if _, ok := seenRecipients[friendID]; ok {
			continue
		}
		seenRecipients[friendID] = struct{}{}
		recipientIDs = append(recipientIDs, friendID)
	}
	friendCount := len(recipientIDs)
	recipientIDs = append(recipientIDs, userID)

	logx.WithContext(ctx).Infow("presence fan-out", logx.Field("user_id", userID), logx.Field("guild_count", len(guildIDs)), logx.Field("friend_count", friendCount), logx.Field("total_recipients", len(recipientIDs)))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(presenceDispatchConcurrency)
	for i := range max(len(guildIDs), len(recipientIDs)) {
		if i < len(guildIDs) {
			guildID := guildIDs[i]
			group.Go(func() error {
				nodes, err := s.resolver.Resolve(groupCtx, discovery.RouteGuild, guildID)
				if err != nil {
					return err
				}
				return s.forEachNode(groupCtx, nodes, func(ctx context.Context, client sessionv1.SessionServiceClient) error {
					req := new(sessionv1.DispatchGuildEventRequest)
					req.SetGuildId(guildID)
					req.SetEvent(protoEvent(event, idempotencyKey))
					_, err := client.DispatchGuildEvent(ctx, req)
					return err
				})
			})
		}
		if i < len(recipientIDs) {
			recipientID := recipientIDs[i]
			group.Go(func() error { return s.dispatchUser(groupCtx, recipientID, event, idempotencyKey) })
		}
	}
	return group.Wait()
}

// friendIDs pages through the user's friendships.
func (s *Server) friendIDs(ctx context.Context, userID int64) ([]int64, error) {
	var friends []int64
	var cursor string
	for {
		req := new(userv1.ListRelationshipsRequest)
		req.SetUserId(userID)
		req.SetType(userv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND)
		if cursor != "" {
			req.SetCursor(cursor)
		}
		req.SetLimit(200)
		resp, err := s.userClient.ListRelationships(ctx, req)
		if err != nil {
			return nil, err
		}
		relationships := resp.GetRelationships()
		if len(relationships) == 0 {
			return friends, nil
		}
		for _, relationship := range relationships {
			friends = append(friends, relationship.GetTargetId())
		}
		if !resp.HasNextCursor() {
			return friends, nil
		}
		cursor = resp.GetNextCursor()
	}
}

func (s *Server) dispatchGuild(ctx context.Context, guildID int64, event eventEnvelope, routing eventRouting, idempotencyKey int64) error {
	nodes, err := s.resolver.Resolve(ctx, discovery.RouteGuild, guildID)
	if err != nil {
		return err
	}
	if event.Type == realtime.EventGuildCreated || event.Type == realtime.EventGuildMemberJoined {
		userID := int64(routing.OwnerID)
		if event.Type == realtime.EventGuildMemberJoined {
			userID = int64(routing.UserID)
		}
		userNodes, err := s.resolver.Resolve(ctx, discovery.RouteUser, userID)
		if err != nil {
			return err
		}
		nodes = mergeNodes(nodes, userNodes)
	}
	return s.forEachNode(ctx, nodes, func(ctx context.Context, client sessionv1.SessionServiceClient) error {
		req := new(sessionv1.DispatchGuildEventRequest)
		req.SetGuildId(guildID)
		req.SetEvent(protoEvent(event, idempotencyKey))
		_, err := client.DispatchGuildEvent(ctx, req)
		return err
	})
}

func (s *Server) forEachNode(
	ctx context.Context,
	nodes []discovery.Node,
	call func(context.Context, sessionv1.SessionServiceClient) error,
) error {
	for _, node := range nodes {
		client, err := s.client(node.RPCAddress)
		if err != nil {
			return err
		}
		callCtx, cancel := context.WithTimeout(ctx, s.dispatchTimeout())
		err = call(callCtx, client)
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) client(address string) (sessionv1.SessionServiceClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if client := s.clients[address]; client != nil {
		return client, nil
	}
	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return nil, err
	}
	client := sessionv1.NewSessionServiceClient(conn)
	s.conns[address] = conn
	s.clients[address] = client
	return client, nil
}

func isCanonicalEventType(eventType string) bool {
	switch eventType {
	case realtime.EventGuildCreated,
		realtime.EventGuildUpdated,
		realtime.EventGuildDeleted,
		realtime.EventGuildMemberJoined,
		realtime.EventGuildMemberUpdated,
		realtime.EventGuildMemberRemoved,
		realtime.EventGuildMemberBanned,
		realtime.EventGuildMemberUnbanned,
		realtime.EventGuildRoleCreated,
		realtime.EventGuildRoleUpdated,
		realtime.EventGuildRoleDeleted,
		realtime.EventGuildMemberRolesUpdated,
		realtime.EventGuildChannelCreated,
		realtime.EventGuildChannelUpdated,
		realtime.EventGuildChannelDeleted,
		realtime.EventGuildChannelOverwriteUpdated,
		realtime.EventGuildChannelOverwriteDeleted,
		realtime.EventMessageCreated,
		realtime.EventMessageUpdated,
		realtime.EventMessageDeleted,
		realtime.EventMessageReadUpdated,
		realtime.EventRelationshipUpdated,
		realtime.EventRelationshipRemoved,
		realtime.EventUserProfileUpdated,
		realtime.EventDmChannelCreated,
		realtime.EventPresenceUpdated,
		realtime.EventPresencePreferenceUpdated:
		return true
	default:
		return false
	}
}

func (s *Server) close() {
	for _, consumer := range s.consumers {
		consumer.client.Close()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, conn := range s.conns {
		_ = conn.Close()
	}
}

func (s *Server) consumerGroup(record *kgo.Record) string {
	if record == nil {
		return ""
	}
	switch record.Topic {
	case defaultString(s.cfg.Kafka.GuildTopic, "cordis.guild.events.v1"):
		return defaultString(s.cfg.Kafka.GuildConsumerGroup, "cordis.dispatcher.guild.v1")
	case defaultString(s.cfg.Kafka.MessageTopic, "cordis.message.events.v1"):
		return defaultString(s.cfg.Kafka.MessageConsumerGroup, "cordis.dispatcher.message.v1")
	case defaultString(s.cfg.Kafka.UserTopic, "cordis.user.events.v1"):
		return defaultString(s.cfg.Kafka.UserConsumerGroup, "cordis.dispatcher.user.v1")
	case defaultString(s.cfg.Kafka.PresenceTopic, "cordis.presence.events.v1"):
		return defaultString(s.cfg.Kafka.PresenceConsumerGroup, "cordis.dispatcher.presence.v1")
	default:
		return ""
	}
}

func (s *Server) dispatchTimeout() time.Duration {
	if s.cfg.Dispatcher.DispatchTimeoutSeconds <= 0 {
		return time.Duration(config.DefaultDispatchTimeoutSeconds) * time.Second
	}
	return time.Duration(s.cfg.Dispatcher.DispatchTimeoutSeconds) * time.Second
}

func (s *Server) revokeTimeout() time.Duration {
	if s.cfg.Dispatcher.RevokeTimeoutSeconds <= 0 {
		return time.Duration(config.DefaultRevokeTimeoutSeconds) * time.Second
	}
	return time.Duration(s.cfg.Dispatcher.RevokeTimeoutSeconds) * time.Second
}

func (s *Server) retryMin() time.Duration {
	if s.cfg.Dispatcher.RetryMinMilliseconds <= 0 {
		return time.Duration(config.DefaultRetryMinMilliseconds) * time.Millisecond
	}
	return time.Duration(s.cfg.Dispatcher.RetryMinMilliseconds) * time.Millisecond
}

func (s *Server) retryMax() time.Duration {
	if s.cfg.Dispatcher.RetryMaxSeconds <= 0 {
		return time.Duration(config.DefaultRetryMaxSeconds) * time.Second
	}
	return time.Duration(s.cfg.Dispatcher.RetryMaxSeconds) * time.Second
}

func commitInterval(milliseconds int) time.Duration {
	if milliseconds <= 0 {
		return time.Duration(config.DefaultCommitIntervalMilliseconds) * time.Millisecond
	}
	return time.Duration(milliseconds) * time.Millisecond
}

func protoEvent(event eventEnvelope, idempotencyKey int64) *sessionv1.EventEnvelope {
	result := new(sessionv1.EventEnvelope)
	result.SetType(event.Type)
	result.SetJsonPayload(string(event.Data))
	result.SetIdempotencyKey(idempotencyKey)
	return result
}

func mergeNodes(groups ...[]discovery.Node) []discovery.Node {
	seen := make(map[string]struct{})
	var result []discovery.Node
	for _, group := range groups {
		for _, node := range group {
			key := node.ID + "\x1f" + node.Generation
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, node)
		}
	}
	return result
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
