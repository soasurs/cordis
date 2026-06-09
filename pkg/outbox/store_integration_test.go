//go:build integration

package outbox

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"sort"
	"testing"
	"testing/fstest"

	"github.com/soasurs/cordis/internal/testpostgres"
	"github.com/soasurs/cordis/pkg/migration"
	messagemigrations "github.com/soasurs/cordis/services/message/v1/db/migrations"
)

func TestClaimBatchClaimsMultipleEventsPerKey(t *testing.T) {
	ctx := t.Context()
	db := testpostgres.New(t, messagemigrations.Files)
	now := Now()

	insert := func(id int64, key string) {
		t.Helper()
		if err := Insert(ctx, db, Event{
			ID:          id,
			Topic:       "message.events",
			Key:         []byte(key),
			Partition:   int(id % 2),
			Payload:     json.RawMessage(`{"event_type":"test"}`),
			MaxRetries:  2,
			AvailableAt: now,
			CreatedAt:   now,
		}); err != nil {
			t.Fatalf("insert event %d: %v", id, err)
		}
	}

	insert(1, "channel-1")
	insert(2, "channel-1")
	insert(3, "channel-2")

	events, err := ClaimBatch(ctx, db, now, 10, 0)
	if err != nil {
		t.Fatalf("claim partition 0: %v", err)
	}
	partitionOneEvents, err := ClaimBatch(ctx, db, now, 10, 1)
	if err != nil {
		t.Fatalf("claim partition 1: %v", err)
	}
	events = append(events, partitionOneEvents...)
	sort.Slice(events, func(i, j int) bool { return events[i].ID < events[j].ID })
	if len(events) != 3 ||
		events[0].ID != 1 ||
		events[1].ID != 2 ||
		events[2].ID != 3 {
		t.Fatalf("claim IDs = %v, want [1 2 3]", eventIDs(events))
	}
	for _, evt := range events {
		if err := MarkSent(ctx, db, evt.ID, now); err != nil {
			t.Fatalf("mark event %d sent: %v", evt.ID, err)
		}
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE outbox_messages
		SET deleted_at = $1
		WHERE id = 2
	`, now-1000); err != nil {
		t.Fatalf("age sent event: %v", err)
	}
	n, err := Cleanup(ctx, db, now, 10)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if n != 1 {
		t.Fatalf("cleanup count = %d, want 1", n)
	}
}

func TestReleaseBackoffDoesNotBlockSameKey(t *testing.T) {
	ctx := t.Context()
	db := testpostgres.New(t, messagemigrations.Files)
	now := Now()

	insert := func(id int64) {
		t.Helper()
		if err := Insert(ctx, db, Event{
			ID:          id,
			Topic:       "message.events",
			Key:         []byte("channel-1"),
			Partition:   1,
			Payload:     json.RawMessage(`{"event_type":"test"}`),
			MaxRetries:  1,
			AvailableAt: now,
			CreatedAt:   now,
		}); err != nil {
			t.Fatalf("insert event %d: %v", id, err)
		}
	}

	insert(10)
	events, err := ClaimBatch(ctx, db, now, 10, 1)
	if err != nil || len(events) != 1 || events[0].ID != 10 {
		t.Fatalf("initial claim = %v, %v", eventIDs(events), err)
	}
	if err := Release(ctx, db, 10, now, now+1000); err != nil {
		t.Fatalf("release initial attempt: %v", err)
	}

	insert(11)
	events, err = ClaimBatch(ctx, db, now, 10, 1)
	if err != nil || len(events) != 1 || events[0].ID != 11 {
		t.Fatalf("claim during prior event backoff = %v, %v", eventIDs(events), err)
	}
	if err := MarkSent(ctx, db, 11, now); err != nil {
		t.Fatalf("mark later event sent: %v", err)
	}

	events, err = ClaimBatch(ctx, db, now+1000, 10, 1)
	if err != nil || len(events) != 1 || events[0].ID != 10 {
		t.Fatalf("retry claim = %v, %v", eventIDs(events), err)
	}
	if err := Release(ctx, db, 10, now+1000, now+3000); err != nil {
		t.Fatalf("release exhausted retry: %v", err)
	}

	var deadAt int64
	if err := db.GetContext(ctx, &deadAt, `SELECT dead_at FROM outbox_messages WHERE id = 10`); err != nil {
		t.Fatalf("read dead event: %v", err)
	}
	if deadAt == 0 {
		t.Fatal("exhausted event was not marked dead")
	}
}

func TestPartitionAdvisoryLocksCoordinateWorkers(t *testing.T) {
	ctx := t.Context()
	db := testpostgres.New(t, messagemigrations.Files)
	now := Now()

	for partition := range 2 {
		for offset := range 2 {
			id := int64(partition*10 + offset + 1)
			if err := Insert(ctx, db, Event{
				ID:          id,
				Topic:       "message.events",
				Key:         []byte("channel"),
				Partition:   partition,
				Payload:     json.RawMessage(`{"event_type":"test"}`),
				MaxRetries:  2,
				AvailableAt: now,
				CreatedAt:   now,
			}); err != nil {
				t.Fatalf("insert partition %d event %d: %v", partition, id, err)
			}
		}
	}

	workerOne, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("worker one conn: %v", err)
	}
	defer workerOne.Close()
	workerTwo, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("worker two conn: %v", err)
	}
	defer workerTwo.Close()

	locked, err := tryPartitionLock(ctx, workerOne, 0)
	if err != nil || !locked {
		t.Fatalf("worker one lock partition 0 = %v, %v", locked, err)
	}
	locked, err = tryPartitionLock(ctx, workerTwo, 0)
	if err != nil {
		t.Fatalf("worker two try partition 0: %v", err)
	}
	if locked {
		t.Fatal("worker two unexpectedly acquired partition 0")
	}

	locked, err = tryPartitionLock(ctx, workerTwo, 1)
	if err != nil || !locked {
		t.Fatalf("worker two lock partition 1 = %v, %v", locked, err)
	}

	workerOneEvents, err := ClaimBatch(ctx, workerOne, now, 10, 0)
	if err != nil {
		t.Fatalf("worker one claim partition 0: %v", err)
	}
	workerTwoEvents, err := ClaimBatch(ctx, workerTwo, now, 10, 1)
	if err != nil {
		t.Fatalf("worker two claim partition 1: %v", err)
	}
	if len(workerOneEvents) != 2 || len(workerTwoEvents) != 2 {
		t.Fatalf("claimed counts = %d, %d, want 2, 2", len(workerOneEvents), len(workerTwoEvents))
	}
	for _, evt := range workerOneEvents {
		if evt.Partition != 0 {
			t.Errorf("worker one claimed partition %d event %d", evt.Partition, evt.ID)
		}
	}
	for _, evt := range workerTwoEvents {
		if evt.Partition != 1 {
			t.Errorf("worker two claimed partition %d event %d", evt.Partition, evt.ID)
		}
	}

	testRelay := &Relay{Logger: slog.Default()}
	testRelay.unlockPartition(workerOne, 0)
	testRelay.unlockPartition(workerTwo, 1)
	locked, err = tryPartitionLock(ctx, workerTwo, 0)
	if err != nil || !locked {
		t.Fatalf("worker two lock released partition 0 = %v, %v", locked, err)
	}
	testRelay.unlockPartition(workerTwo, 0)
}

func TestMigrationsAreRepeatableAndKeepOutboxData(t *testing.T) {
	ctx := t.Context()
	db := testpostgres.New(t, messagemigrations.Files)
	now := Now()

	if err := Insert(ctx, db, Event{
		ID:          20,
		Topic:       "message.events",
		Key:         []byte("channel-1"),
		Partition:   3,
		Payload:     json.RawMessage(`{"event_type":"test"}`),
		MaxRetries:  1,
		AvailableAt: now,
		CreatedAt:   now,
	}); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	if err := migration.Apply(ctx, db, messagemigrations.Files); err != nil {
		t.Fatalf("reapply migrations: %v", err)
	}

	var count int
	if err := db.GetContext(ctx, &count, `SELECT COUNT(*) FROM outbox_messages WHERE id = 20`); err != nil {
		t.Fatalf("count retained event: %v", err)
	}
	if count != 1 {
		t.Fatalf("retained event count = %d, want 1", count)
	}
}

func TestPartitionMigrationBackfillsLegacyRows(t *testing.T) {
	partitionMigration, err := fs.ReadFile(
		messagemigrations.Files,
		"000004_outbox_partitions.up.sql",
	)
	if err != nil {
		t.Fatalf("read partition migration: %v", err)
	}

	legacyMigrations := fstest.MapFS{
		"000001_legacy_outbox.sql": &fstest.MapFile{Data: []byte(`
			CREATE TABLE outbox_messages (
				id           BIGINT PRIMARY KEY,
				topic        TEXT NOT NULL,
				key          BYTEA,
				payload      JSONB NOT NULL,
				retry_count  INT NOT NULL DEFAULT 0,
				max_retries  INT NOT NULL DEFAULT 5,
				available_at BIGINT NOT NULL DEFAULT 0,
				locked_at    BIGINT NOT NULL DEFAULT 0,
				dead_at      BIGINT NOT NULL DEFAULT 0,
				deleted_at   BIGINT NOT NULL DEFAULT 0,
				created_at   BIGINT NOT NULL
			);
			INSERT INTO outbox_messages (
				id, topic, key, payload, available_at, created_at
			) VALUES
				(1, 'message.events', convert_to('65', 'UTF8'), '{}', 1, 1),
				(2, 'message.events', convert_to('130', 'UTF8'), '{}', 1, 1);
		`)},
		"000004_outbox_partitions.up.sql": &fstest.MapFile{Data: partitionMigration},
	}

	db := testpostgres.New(t, legacyMigrations)
	rows, err := db.QueryContext(t.Context(), `
		SELECT partition_id
		FROM outbox_messages
		ORDER BY id
	`)
	if err != nil {
		t.Fatalf("query backfilled partitions: %v", err)
	}
	defer rows.Close()

	var partitions []int
	for rows.Next() {
		var partition int
		if err := rows.Scan(&partition); err != nil {
			t.Fatalf("scan partition: %v", err)
		}
		partitions = append(partitions, partition)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate partitions: %v", err)
	}
	if len(partitions) != 2 || partitions[0] != 1 || partitions[1] != 2 {
		t.Fatalf("backfilled partitions = %v, want [1 2]", partitions)
	}
}

func eventIDs(events []Event) []int64 {
	ids := make([]int64, 0, len(events))
	for _, evt := range events {
		ids = append(ids, evt.ID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
