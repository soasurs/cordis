package store

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) (*SQLStore, sqlmock.Sqlmock, func()) {
	t.Helper()

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)

	sqlxDB := sqlx.NewDb(db, "postgres")
	return &SQLStore{db: sqlxDB, q: sqlxDB}, mock, func() {
		require.NoError(t, mock.ExpectationsWereMet())
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
	require.NoError(t, err)
	require.Equal(t, int64(2001), session.SessionID)
	require.Equal(t, int64(1001), session.UserID)
	require.Equal(t, "refresh-hash", session.RefreshTokenHash)
	require.Equal(t, "agent", session.UserAgent)
	require.Equal(t, "127.0.0.1", session.IP)
	require.Equal(t, int64(3000), session.ExpiresAt)
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
	require.NoError(t, err)
	require.Equal(t, int64(2001), session.SessionID)
	require.Equal(t, int64(1001), session.UserID)
	require.Equal(t, "refresh-hash", session.RefreshTokenHash)
}

func TestListSessions(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{
		"session_id", "user_id", "refresh_token_hash", "user_agent", "ip", "created_at", "updated_at", "expires_at", "revoked_at",
	}).
		AddRow(int64(2002), int64(1001), "refresh-hash-2", "agent-2", "127.0.0.2", int64(20), int64(0), int64(4000), int64(0)).
		AddRow(int64(2001), int64(1001), "refresh-hash-1", "agent-1", "127.0.0.1", int64(10), int64(0), int64(3000), int64(0))

	mock.ExpectQuery(sqlPattern(ListSessionsQuery)).
		WithArgs(int64(1001), 0, sqlmock.AnyArg()).
		WillReturnRows(rows)

	sessions, err := store.ListSessions(context.Background(), 1001)
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	require.Equal(t, int64(2002), sessions[0].SessionID)
}

func TestRotateRefreshToken(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(sqlPattern(RotateRefreshTokenStatement)).
		WithArgs("new-refresh-hash", sqlmock.AnyArg(), int64(2001), 0, "old-refresh-hash").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, store.RotateRefreshToken(context.Background(), 2001, "old-refresh-hash", "new-refresh-hash"))
}

func TestRotateRefreshTokenNoRows(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(sqlPattern(RotateRefreshTokenStatement)).
		WithArgs("new-refresh-hash", sqlmock.AnyArg(), int64(2001), 0, "old-refresh-hash").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := store.RotateRefreshToken(context.Background(), 2001, "old-refresh-hash", "new-refresh-hash")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestRevokeSession(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(sqlPattern(RevokeSessionStatement)).
		WithArgs(sqlmock.AnyArg(), int64(2001), 0).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, store.RevokeSession(context.Background(), 2001))
}

func TestRevokeUserSession(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(sqlPattern(RevokeUserSessionStatement)).
		WithArgs(sqlmock.AnyArg(), int64(1001), int64(2001), 0).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, store.RevokeUserSession(context.Background(), 1001, 2001))
}

func TestRevokeOtherSessions(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(sqlPattern(RevokeOtherSessionsStatement)).
		WithArgs(sqlmock.AnyArg(), int64(1001), int64(2001), 0).
		WillReturnResult(sqlmock.NewResult(0, 2))

	revoked, err := store.RevokeOtherSessions(context.Background(), 1001, 2001)
	require.NoError(t, err)
	require.Equal(t, int64(2), revoked)
}
