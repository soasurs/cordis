package outbox

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func newTestDB(t *testing.T) (*sqlx.DB, sqlmock.Sqlmock, func()) {
	t.Helper()

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}

	sqlxDB := sqlx.NewDb(db, "postgres")
	return sqlxDB, mock, func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet sql expectations: %v", err)
		}
		_ = sqlxDB.Close()
	}
}

func sqlPattern(query string) string {
	fields := strings.Fields(query)
	for i := range fields {
		fields[i] = regexp.QuoteMeta(fields[i])
	}
	return strings.Join(fields, `\s+`)
}

func insertPattern() string {
	return sqlPattern(`
	INSERT INTO outbox_messages (
		id, topic, key, partition_id, payload, retry_count, max_retries,
		available_at, locked_at, dead_at, deleted_at, created_at
	) VALUES (
		$1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10, $11, $12
	)
	`)
}

func releasePattern() string {
	return sqlPattern(`
	UPDATE outbox_messages
	SET
		locked_at = 0,
		retry_count = retry_count + 1,
		available_at = CASE
				WHEN retry_count + 1 <= max_retries THEN $2
			ELSE available_at
		END,
		dead_at = CASE
				WHEN retry_count + 1 > max_retries THEN $1
			ELSE dead_at
		END
	WHERE id = $3 AND deleted_at = 0
	`)
}

func markSentPattern() string {
	return sqlPattern(`
	UPDATE outbox_messages
	SET deleted_at = $1, locked_at = 0
	WHERE id = $2 AND dead_at = 0
	`)
}

func recoverStalePattern() string {
	return sqlPattern(`
	UPDATE outbox_messages
	SET locked_at = 0
	WHERE locked_at > 0
	  AND locked_at < $1
	  AND dead_at = 0
	  AND deleted_at = 0
	  AND retry_count <= max_retries
	  AND partition_id = $2
	`)
}

func cleanupPattern() string {
	return sqlPattern(`
	WITH expired AS (
		SELECT id
		FROM outbox_messages
		WHERE deleted_at > 0 AND deleted_at < $1
		ORDER BY deleted_at
		LIMIT $2
	)
	DELETE FROM outbox_messages
	WHERE id IN (SELECT id FROM expired)
	`)
}

func readyPartitionsPattern() string {
	return sqlPattern(`
	SELECT partition_id
	FROM outbox_messages
	WHERE locked_at = 0
	  AND dead_at = 0
	  AND deleted_at = 0
	  AND retry_count <= max_retries
	  AND available_at <= $1
	GROUP BY partition_id
	ORDER BY partition_id
	LIMIT $2
	`)
}

func TestInsert(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	payload := json.RawMessage(`{"type":"test"}`)

	mock.ExpectExec(insertPattern()).
		WithArgs(
			int64(1), "test.topic", []byte("key-1"), 3, []byte(payload), 0, 5,
			sqlmock.AnyArg(), int64(0), int64(0), int64(0), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	now := Now()
	evt := Event{
		ID:          1,
		Topic:       "test.topic",
		Key:         []byte("key-1"),
		Partition:   3,
		Payload:     payload,
		RetryCount:  0,
		MaxRetries:  5,
		AvailableAt: now,
		CreatedAt:   now,
	}
	if err := Insert(t.Context(), db, evt); err != nil {
		t.Fatalf("Insert returned error: %v", err)
	}
}

func TestInsertNilKey(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	payload := json.RawMessage(`{"type":"test"}`)

	mock.ExpectExec(insertPattern()).
		WithArgs(
			int64(2), "test.topic", sqlmock.AnyArg(), 0, []byte(payload), 0, 5,
			sqlmock.AnyArg(), int64(0), int64(0), int64(0), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	now := Now()
	evt := Event{
		ID:          2,
		Topic:       "test.topic",
		Key:         nil,
		Payload:     payload,
		RetryCount:  0,
		MaxRetries:  5,
		AvailableAt: now,
		CreatedAt:   now,
	}
	if err := Insert(t.Context(), db, evt); err != nil {
		t.Fatalf("Insert with nil key returned error: %v", err)
	}
}

func TestClaimBatch(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	payload := []byte(`{"type":"test"}`)

	claimBatchQuery := `
	UPDATE outbox_messages AS claimed
	SET locked_at = $1
	WHERE id IN (
		SELECT candidate.id
		FROM outbox_messages AS candidate
		WHERE candidate.locked_at = 0
		  AND candidate.dead_at = 0
		  AND candidate.deleted_at = 0
		  AND candidate.retry_count <= candidate.max_retries
		  AND candidate.available_at <= $1
		  AND candidate.partition_id = $2
		ORDER BY candidate.id
		LIMIT $3
		FOR UPDATE OF candidate SKIP LOCKED
	)
	RETURNING id, topic, key, partition_id, payload, retry_count, max_retries,
		available_at, locked_at, dead_at, deleted_at, created_at
	`

	rows := sqlmock.NewRows([]string{
		"id", "topic", "key", "partition_id", "payload", "retry_count", "max_retries",
		"available_at", "locked_at", "dead_at", "deleted_at", "created_at",
	}).AddRow(
		int64(2), "test.topic", []byte("key-2"), 2, payload, 1, 5,
		int64(0), int64(1700000000000), int64(0), int64(0), int64(1700000000000),
	).AddRow(
		int64(1), "test.topic", []byte("key-1"), 2, payload, 0, 5,
		int64(0), int64(1700000000000), int64(0), int64(0), int64(1700000000000),
	)

	mock.ExpectQuery(sqlPattern(claimBatchQuery)).
		WithArgs(sqlmock.AnyArg(), 2, 10).
		WillReturnRows(rows)

	events, err := ClaimBatch(t.Context(), db, Now(), 10, 2)
	if err != nil {
		t.Fatalf("ClaimBatch returned error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("ClaimBatch returned %d events, want 2", len(events))
	}
	if events[0].ID != 1 || events[1].ID != 2 {
		t.Fatalf("unexpected event IDs: %d, %d", events[0].ID, events[1].ID)
	}
	if events[1].RetryCount != 1 {
		t.Fatalf("unexpected retry count: %d", events[1].RetryCount)
	}
	if events[0].Partition != 2 || events[1].Partition != 2 {
		t.Fatalf("unexpected partitions: %d, %d", events[0].Partition, events[1].Partition)
	}
}

func TestClaimBatchEmpty(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	claimBatchQuery := `
	UPDATE outbox_messages AS claimed
	SET locked_at = $1
	WHERE id IN (
		SELECT candidate.id
		FROM outbox_messages AS candidate
		WHERE candidate.locked_at = 0
		  AND candidate.dead_at = 0
		  AND candidate.deleted_at = 0
		  AND candidate.retry_count <= candidate.max_retries
		  AND candidate.available_at <= $1
		  AND candidate.partition_id = $2
		ORDER BY candidate.id
		LIMIT $3
		FOR UPDATE OF candidate SKIP LOCKED
	)
	RETURNING id, topic, key, partition_id, payload, retry_count, max_retries,
		available_at, locked_at, dead_at, deleted_at, created_at
	`

	rows := sqlmock.NewRows([]string{
		"id", "topic", "key", "partition_id", "payload", "retry_count", "max_retries",
		"available_at", "locked_at", "dead_at", "deleted_at", "created_at",
	})

	mock.ExpectQuery(sqlPattern(claimBatchQuery)).
		WithArgs(sqlmock.AnyArg(), 3, 5).
		WillReturnRows(rows)

	events, err := ClaimBatch(t.Context(), db, Now(), 5, 3)
	if err != nil {
		t.Fatalf("ClaimBatch returned error: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("ClaimBatch returned %d events, want 0", len(events))
	}
}

func TestReadyPartitions(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	mock.ExpectQuery(readyPartitionsPattern()).
		WithArgs(int64(1700000000000), 64).
		WillReturnRows(sqlmock.NewRows([]string{"partition_id"}).
			AddRow(3).
			AddRow(7))

	partitions, err := ReadyPartitions(t.Context(), db, 1700000000000, 64)
	if err != nil {
		t.Fatalf("ReadyPartitions returned error: %v", err)
	}
	if len(partitions) != 2 || partitions[0] != 3 || partitions[1] != 7 {
		t.Fatalf("ReadyPartitions = %v, want [3 7]", partitions)
	}
}

func TestRelease(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	mock.ExpectExec(releasePattern()).
		WithArgs(int64(1700000000000), int64(1700000001000), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := Release(t.Context(), db, 1, 1700000000000, 1700000001000); err != nil {
		t.Fatalf("Release returned error: %v", err)
	}
}

func TestMarkSent(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	now := Now()
	mock.ExpectExec(markSentPattern()).
		WithArgs(now, int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := MarkSent(t.Context(), db, 1, now); err != nil {
		t.Fatalf("MarkSent returned error: %v", err)
	}
}

func TestCleanup(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	mock.ExpectExec(cleanupPattern()).
		WithArgs(int64(1700000000000), 10000).
		WillReturnResult(sqlmock.NewResult(3, 3))

	n, err := Cleanup(t.Context(), db, 1700000000000, 10000)
	if err != nil {
		t.Fatalf("Cleanup returned error: %v", err)
	}
	if n != 3 {
		t.Fatalf("Cleanup returned %d, want 3", n)
	}
}

func TestRecoverStale(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	mock.ExpectExec(recoverStalePattern()).
		WithArgs(int64(1700000000000), 2).
		WillReturnResult(sqlmock.NewResult(2, 2))

	if err := RecoverStale(t.Context(), db, 1700000000000, 2); err != nil {
		t.Fatalf("RecoverStale returned error: %v", err)
	}
}
