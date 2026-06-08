package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func newTestStore(t *testing.T) (*SQLStore, sqlmock.Sqlmock, func()) {
	t.Helper()

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}

	sqlxDB := sqlx.NewDb(db, "postgres")
	return &SQLStore{db: sqlxDB, q: sqlxDB}, mock, func() {
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

func createSessionExecPattern() string {
	return sqlPattern(`
	INSERT INTO
		sessions (session_id, user_id, refresh_token_hash, user_agent, ip, created_at, updated_at, expires_at, revoked_at)
	VALUES
		($1, $2, $3, $4, $5, $6, $7, $8, $9);
	`)
}

func TestCreateSession(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(createSessionExecPattern()).
		WithArgs(int64(2001), int64(1001), "refresh-hash", "agent", "127.0.0.1", sqlmock.AnyArg(), int64(0), int64(3000), int64(0)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	session, err := store.CreateSession(context.Background(), 2001, 1001, "refresh-hash", "agent", "127.0.0.1", 3000)
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if session.SessionID != 2001 || session.UserID != 1001 || session.RefreshTokenHash != "refresh-hash" {
		t.Fatalf("unexpected session: %+v", session)
	}
	if session.UserAgent != "agent" || session.IP != "127.0.0.1" || session.ExpiresAt != 3000 {
		t.Fatalf("unexpected session metadata: %+v", session)
	}
}

func TestGetSession(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{
		"session_id", "user_id", "refresh_token_hash", "user_agent", "ip", "created_at", "updated_at", "expires_at", "revoked_at",
	}).AddRow(int64(2001), int64(1001), "refresh-hash", "agent", "127.0.0.1", int64(10), int64(20), int64(3000), int64(0))

	mock.ExpectQuery(sqlPattern(GetSessionQuery)).
		WithArgs(int64(2001)).
		WillReturnRows(rows)

	session, err := store.GetSession(context.Background(), 2001)
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}
	if session.SessionID != 2001 || session.UserID != 1001 || session.RefreshTokenHash != "refresh-hash" {
		t.Fatalf("unexpected session: %+v", session)
	}
}

func TestRotateRefreshToken(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(sqlPattern(RotateRefreshTokenStatement)).
		WithArgs("new-refresh-hash", sqlmock.AnyArg(), int64(2001), 0, "old-refresh-hash").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := store.RotateRefreshToken(context.Background(), 2001, "old-refresh-hash", "new-refresh-hash"); err != nil {
		t.Fatalf("RotateRefreshToken returned error: %v", err)
	}
}

func TestRotateRefreshTokenNoRows(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(sqlPattern(RotateRefreshTokenStatement)).
		WithArgs("new-refresh-hash", sqlmock.AnyArg(), int64(2001), 0, "old-refresh-hash").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := store.RotateRefreshToken(context.Background(), 2001, "old-refresh-hash", "new-refresh-hash")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestRevokeSession(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(sqlPattern(RevokeSessionStatement)).
		WithArgs(sqlmock.AnyArg(), int64(2001), 0).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := store.RevokeSession(context.Background(), 2001); err != nil {
		t.Fatalf("RevokeSession returned error: %v", err)
	}
}
