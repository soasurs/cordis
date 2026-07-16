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

func TestCreateGuildMember(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{
		"guild_id", "user_id", "nickname", "revision", "joined_at", "updated_at", "deleted_at",
	}).AddRow(int64(1001), int64(2001), "", int64(1), int64(10), int64(0), int64(0))
	mock.ExpectQuery(sqlPattern(createGuildMemberQuery)).
		WithArgs(int64(1001), int64(2001), int64(10)).
		WillReturnRows(rows)

	member, err := store.CreateGuildMember(context.Background(), 1001, 2001, 10)
	require.NoError(t, err)
	require.Equal(t, int64(2001), member.UserID)
	require.Equal(t, int64(1), member.Revision)
}

func TestCreateGuildMemberAlreadyExists(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectQuery(sqlPattern(createGuildMemberQuery)).
		WithArgs(int64(1001), int64(2001), int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{
			"guild_id", "user_id", "nickname", "revision", "joined_at", "updated_at", "deleted_at",
		}))

	_, err := store.CreateGuildMember(context.Background(), 1001, 2001, 10)
	require.ErrorIs(t, err, ErrMemberAlreadyExists)
}

func TestUpdateGuildMemberNickname(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{
		"guild_id", "user_id", "nickname", "revision", "joined_at", "updated_at", "deleted_at",
	}).AddRow(int64(1001), int64(2001), "member", int64(2), int64(10), int64(20), int64(0))
	mock.ExpectQuery(sqlPattern(updateGuildMemberNicknameQuery)).
		WithArgs(int64(1001), int64(2001), "member", sqlmock.AnyArg()).
		WillReturnRows(rows)

	member, err := store.UpdateGuildMemberNickname(context.Background(), 1001, 2001, "member")
	require.NoError(t, err)
	require.Equal(t, "member", member.Nickname)
	require.Equal(t, int64(2), member.Revision)
}

func TestRemoveGuildMember(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{
		"guild_id", "user_id", "nickname", "revision", "joined_at", "updated_at", "deleted_at",
	}).AddRow(int64(1001), int64(2001), "", int64(2), int64(10), int64(20), int64(20))
	mock.ExpectQuery(sqlPattern(removeGuildMemberQuery)).
		WithArgs(int64(1001), int64(2001), int64(20)).
		WillReturnRows(rows)

	member, err := store.RemoveGuildMember(context.Background(), 1001, 2001, 20)
	require.NoError(t, err)
	require.Equal(t, int64(20), member.DeletedAt)
	require.Equal(t, int64(2), member.Revision)
}

func TestTransferGuildOwnership(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{
		"id", "owner_id", "name", "icon_uri", "revision", "created_at", "updated_at", "deleted_at",
	}).AddRow(int64(1001), int64(2002), "Cordis", "", int64(2), int64(10), int64(20), int64(0))
	mock.ExpectQuery(sqlPattern(transferGuildOwnershipQuery)).
		WithArgs(int64(1001), int64(2001), int64(2002), sqlmock.AnyArg()).
		WillReturnRows(rows)

	guild, err := store.TransferGuildOwnership(context.Background(), 1001, 2001, 2002)
	require.NoError(t, err)
	require.Equal(t, int64(2002), guild.OwnerID)
	require.Equal(t, int64(2), guild.Revision)
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
