package store

import (
	"context"
	"errors"
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

func TestCreateGuild(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{
		"id", "owner_id", "name", "icon_uri", "revision", "created_at", "updated_at", "deleted_at",
	}).AddRow(int64(1001), int64(2001), "Cordis", "", int64(1), int64(10), int64(0), int64(0))
	mock.ExpectQuery(sqlPattern(createGuildQuery)).
		WithArgs(int64(1001), int64(2001), "Cordis", "", int64(10)).
		WillReturnRows(rows)

	guild, err := store.CreateGuild(context.Background(), 1001, 2001, "Cordis", "", 10)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guild.ID)
	require.Equal(t, int64(2001), guild.OwnerID)
	require.Equal(t, int64(1), guild.Revision)
}

func TestGetGuildForMember(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{
		"id", "owner_id", "name", "icon_uri", "revision", "created_at", "updated_at", "deleted_at",
	}).AddRow(int64(1001), int64(2001), "Cordis", "", int64(1), int64(10), int64(0), int64(0))
	mock.ExpectQuery(sqlPattern(getGuildForMemberQuery)).
		WithArgs(int64(1001), int64(2001)).
		WillReturnRows(rows)

	guild, err := store.GetGuildForMember(context.Background(), 1001, 2001)
	require.NoError(t, err)
	require.Equal(t, "Cordis", guild.Name)
}

func TestTransactRollback(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	errRollback := errors.New("rollback")
	mock.ExpectBegin()
	mock.ExpectRollback()
	err := store.Transact(context.Background(), func(Store) error {
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
}
