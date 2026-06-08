package outbox

import (
	"context"
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
		id, topic, key, payload, retry_count, max_retries,
		locked_at, deleted_at, created_at
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9
	)
	`)
}

func releasePattern() string {
	return sqlPattern(`
	UPDATE outbox_messages
	SET locked_at = 0, retry_count = retry_count + 1
	WHERE id = $1
	`)
}

func markSentPattern() string {
	return sqlPattern(`
	UPDATE outbox_messages
	SET deleted_at = $1
	WHERE id = $2
	`)
}

func recoverStalePattern() string {
	return sqlPattern(`
	UPDATE outbox_messages
	SET locked_at = 0
	WHERE locked_at > 0
	  AND locked_at < $1
	  AND retry_count < max_retries
	`)
}

func dropDeadPattern() string {
	return sqlPattern(`
	DELETE FROM outbox_messages
	WHERE retry_count >= max_retries AND created_at < $1
	`)
}

func cleanupPattern() string {
	return sqlPattern(`
	DELETE FROM outbox_messages
	WHERE deleted_at > 0 AND deleted_at < $1
	LIMIT $2
	`)
}

func TestInsert(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	payload := json.RawMessage(`{"type":"test"}`)

	mock.ExpectExec(insertPattern()).
		WithArgs(int64(1), "test.topic", []byte("key-1"), []byte(payload), 0, 5, int64(0), int64(0), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	evt := Event{
		ID:         1,
		Topic:      "test.topic",
		Key:        []byte("key-1"),
		Payload:    payload,
		RetryCount: 0,
		MaxRetries: 5,
		LockedAt:   0,
		DeletedAt:  0,
		CreatedAt:  Now(),
	}
	if err := Insert(context.Background(), db, evt); err != nil {
		t.Fatalf("Insert returned error: %v", err)
	}
}

func TestInsertNilKey(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	payload := json.RawMessage(`{"type":"test"}`)

	mock.ExpectExec(insertPattern()).
		WithArgs(int64(2), "test.topic", sqlmock.AnyArg(), []byte(payload), 0, 5, int64(0), int64(0), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	evt := Event{
		ID:         2,
		Topic:      "test.topic",
		Key:        nil,
		Payload:    payload,
		RetryCount: 0,
		MaxRetries: 5,
		LockedAt:   0,
		DeletedAt:  0,
		CreatedAt:  Now(),
	}
	if err := Insert(context.Background(), db, evt); err != nil {
		t.Fatalf("Insert with nil key returned error: %v", err)
	}
}

func TestClaimBatch(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	payload := []byte(`{"type":"test"}`)

	claimBatchQuery := `UPDATE outbox_messages SET locked_at = $1 ` +
		`WHERE id IN ( SELECT id FROM outbox_messages WHERE locked_at = 0 ` +
		`ORDER BY id LIMIT $2 FOR UPDATE SKIP LOCKED ) ` +
		`RETURNING id, topic, key, payload, retry_count, max_retries, locked_at, deleted_at, created_at`

	rows := sqlmock.NewRows([]string{
		"id", "topic", "key", "payload", "retry_count", "max_retries",
		"locked_at", "deleted_at", "created_at",
	}).AddRow(
		int64(1), "test.topic", []byte("key-1"), payload, 0, 5,
		int64(1700000000000), int64(0), int64(1700000000000),
	).AddRow(
		int64(2), "test.topic", []byte("key-2"), payload, 1, 5,
		int64(1700000000000), int64(0), int64(1700000000000),
	)

	mock.ExpectQuery(sqlPattern(claimBatchQuery)).
		WithArgs(sqlmock.AnyArg(), 10).
		WillReturnRows(rows)

	events, err := ClaimBatch(context.Background(), db, Now(), 10)
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
}

func TestClaimBatchEmpty(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	claimBatchQuery := `UPDATE outbox_messages SET locked_at = $1 ` +
		`WHERE id IN ( SELECT id FROM outbox_messages WHERE locked_at = 0 ` +
		`ORDER BY id LIMIT $2 FOR UPDATE SKIP LOCKED ) ` +
		`RETURNING id, topic, key, payload, retry_count, max_retries, locked_at, deleted_at, created_at`

	rows := sqlmock.NewRows([]string{
		"id", "topic", "key", "payload", "retry_count", "max_retries",
		"locked_at", "deleted_at", "created_at",
	})

	mock.ExpectQuery(sqlPattern(claimBatchQuery)).
		WithArgs(sqlmock.AnyArg(), 5).
		WillReturnRows(rows)

	events, err := ClaimBatch(context.Background(), db, Now(), 5)
	if err != nil {
		t.Fatalf("ClaimBatch returned error: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("ClaimBatch returned %d events, want 0", len(events))
	}
}

func TestRelease(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	mock.ExpectExec(releasePattern()).
		WithArgs(int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := Release(context.Background(), db, 1); err != nil {
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

	if err := MarkSent(context.Background(), db, 1, now); err != nil {
		t.Fatalf("MarkSent returned error: %v", err)
	}
}

func TestCleanup(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	mock.ExpectExec(cleanupPattern()).
		WithArgs(int64(1700000000000), 10000).
		WillReturnResult(sqlmock.NewResult(3, 3))

	n, err := Cleanup(context.Background(), db, 1700000000000, 10000)
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
		WithArgs(int64(1700000000000)).
		WillReturnResult(sqlmock.NewResult(2, 2))

	if err := RecoverStale(context.Background(), db, 1700000000000); err != nil {
		t.Fatalf("RecoverStale returned error: %v", err)
	}
}

func TestDropDead(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	mock.ExpectExec(dropDeadPattern()).
		WithArgs(int64(1700000000000)).
		WillReturnResult(sqlmock.NewResult(3, 3))

	n, err := DropDead(context.Background(), db, 1700000000000)
	if err != nil {
		t.Fatalf("DropDead returned error: %v", err)
	}
	if n != 3 {
		t.Fatalf("DropDead returned %d, want 3", n)
	}
}
