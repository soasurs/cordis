//go:build integration

package outbox

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/internal/testpostgres"
	messagemigrations "github.com/soasurs/cordis/services/message/v1/db/migrations"
)

func TestRelayProducesEvents(t *testing.T) {
	ctx := t.Context()
	db := testpostgres.New(t, messagemigrations.Files)
	producer := &FakeProducer{}
	cfg := DefaultRelayConfig()
	cfg.PollInterval = 10 * time.Millisecond

	relay := NewRelay(cfg, db, producer)
	relay.Start(ctx)
	defer func() {
		relay.Stop()
		relay.WaitCallbacks()
	}()

	now := Now()
	require.NoError(t, Insert(ctx, db, Event{
		ID:          1,
		Topic:       "test.topic",
		Key:         []byte("key-1"),
		Partition:   0,
		Payload:     json.RawMessage(`{"type":"created"}`),
		MaxRetries:  5,
		AvailableAt: now,
		CreatedAt:   now,
	}))

	relay.Notify()

	require.Eventually(t, func() bool {
		return len(producer.Records()) >= 1
	}, 5*time.Second, 50*time.Millisecond, "expected event to be produced")

	records := producer.Records()
	require.Len(t, records, 1)
	require.Equal(t, "test.topic", records[0].Topic)
	require.Equal(t, []byte("key-1"), records[0].Key)
	require.JSONEq(t, `{"type":"created"}`, string(records[0].Value))
}

func TestRelayMarksEventSent(t *testing.T) {
	ctx := t.Context()
	db := testpostgres.New(t, messagemigrations.Files)
	producer := &FakeProducer{}
	cfg := DefaultRelayConfig()
	cfg.PollInterval = 10 * time.Millisecond

	relay := NewRelay(cfg, db, producer)
	relay.Start(ctx)
	defer func() {
		relay.Stop()
		relay.WaitCallbacks()
	}()

	now := Now()
	require.NoError(t, Insert(ctx, db, Event{
		ID:          10,
		Topic:       "test.topic",
		Partition:   0,
		Payload:     json.RawMessage(`{}`),
		MaxRetries:  5,
		AvailableAt: now,
		CreatedAt:   now,
	}))

	relay.Notify()

	require.Eventually(t, func() bool {
		return len(producer.Records()) >= 1
	}, 5*time.Second, 50*time.Millisecond)

	// Verify the event was soft-deleted (marked sent).
	var deletedAt int64
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT deleted_at FROM outbox_messages WHERE id = 10`,
	).Scan(&deletedAt))
	require.Greater(t, deletedAt, int64(0), "event should be marked as sent")
}

func TestRelayReleasesOnProduceFailure(t *testing.T) {
	ctx := t.Context()
	db := testpostgres.New(t, messagemigrations.Files)
	producer := &FakeProducer{
		Err: errProduceFailed,
	}
	cfg := DefaultRelayConfig()
	cfg.PollInterval = 10 * time.Millisecond
	cfg.MaxRetries = 2

	relay := NewRelay(cfg, db, producer)
	relay.Start(ctx)
	defer func() {
		relay.Stop()
		relay.WaitCallbacks()
	}()

	now := Now()
	require.NoError(t, Insert(ctx, db, Event{
		ID:          20,
		Topic:       "test.topic",
		Partition:   0,
		Payload:     json.RawMessage(`{}`),
		MaxRetries:  2,
		AvailableAt: now,
		CreatedAt:   now,
	}))

	relay.Notify()

	// Wait for the produce attempt to happen.
	require.Eventually(t, func() bool {
		return len(producer.Records()) >= 1
	}, 5*time.Second, 50*time.Millisecond)

	// Event should have been released (retry_count incremented, locked_at reset).
	var retryCount int
	var lockedAt int64
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT retry_count, locked_at FROM outbox_messages WHERE id = 20`,
	).Scan(&retryCount, &lockedAt))
	require.Equal(t, 1, retryCount, "retry count should be incremented")
	require.Equal(t, int64(0), lockedAt, "locked_at should be reset")
}

func TestRelayDeadLettersAfterMaxRetries(t *testing.T) {
	ctx := t.Context()
	db := testpostgres.New(t, messagemigrations.Files)
	producer := &FakeProducer{
		Err: errProduceFailed,
	}
	cfg := DefaultRelayConfig()
	cfg.PollInterval = 10 * time.Millisecond
	cfg.MaxRetries = 0 // fail on first attempt = dead letter

	relay := NewRelay(cfg, db, producer)
	relay.Start(ctx)
	defer func() {
		relay.Stop()
		relay.WaitCallbacks()
	}()

	now := Now()
	require.NoError(t, Insert(ctx, db, Event{
		ID:          30,
		Topic:       "test.topic",
		Partition:   0,
		Payload:     json.RawMessage(`{}`),
		MaxRetries:  0,
		AvailableAt: now,
		CreatedAt:   now,
	}))

	relay.Notify()

	require.Eventually(t, func() bool {
		return len(producer.Records()) >= 1
	}, 5*time.Second, 50*time.Millisecond)

	// Wait for the release callback to complete.
	relay.WaitCallbacks()

	var deadAt int64
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT dead_at FROM outbox_messages WHERE id = 30`,
	).Scan(&deadAt))
	require.Greater(t, deadAt, int64(0), "event should be dead-lettered")
}

func TestRelayMultipleEventsInBatch(t *testing.T) {
	ctx := t.Context()
	db := testpostgres.New(t, messagemigrations.Files)
	producer := &FakeProducer{}
	cfg := DefaultRelayConfig()
	cfg.PollInterval = 10 * time.Millisecond
	cfg.BatchSize = 10

	relay := NewRelay(cfg, db, producer)
	relay.Start(ctx)
	defer func() {
		relay.Stop()
		relay.WaitCallbacks()
	}()

	now := Now()
	for i := int64(1); i <= 5; i++ {
		require.NoError(t, Insert(ctx, db, Event{
			ID:          i,
			Topic:       "test.topic",
			Partition:   0,
			Payload:     json.RawMessage(`{}`),
			MaxRetries:  5,
			AvailableAt: now,
			CreatedAt:   now,
		}))
	}

	relay.Notify()

	require.Eventually(t, func() bool {
		return len(producer.Records()) >= 5
	}, 5*time.Second, 50*time.Millisecond, "expected all 5 events to be produced")

	require.Len(t, producer.Records(), 5)
}

// errProduceFailed is a sentinel for test produce failures.
var errProduceFailed = &testError{"produce failed"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
