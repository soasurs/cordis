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

func createUserExecPattern() string {
	return sqlPattern(`
	INSERT INTO
		users (user_id, email, hashed_password, created_at, updated_at, deleted_at)
	VALUES
		($1, $2, $3, $4, $5, $6);
	`)
}

func createUserProfileExecPattern() string {
	return sqlPattern(`
	INSERT INTO
		user_profiles (user_id, name, avatar_uri, created_at, updated_at, deleted_at)
	VALUES
		($1, $2, $3, $4, $5, $6);
	`)
}

func TestCreateUser(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(createUserExecPattern()).
		WithArgs(int64(1001), "user@example.com", "hashed-password", sqlmock.AnyArg(), int64(0), int64(0)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	user, err := store.CreateUser(context.Background(), 1001, "user@example.com", "hashed-password")
	require.NoError(t, err)
	require.Equal(t, int64(1001), user.UserID)
	require.Equal(t, "user@example.com", user.Email)
	require.Equal(t, "hashed-password", user.HashedPassword)
	require.NotZero(t, user.CreatedAt)
	require.Zero(t, user.UpdatedAt)
	require.Zero(t, user.DeletedAt)
}

func TestGetUser(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"user_id", "email", "hashed_password", "created_at", "updated_at", "deleted_at"}).
		AddRow(int64(1001), "user@example.com", "hashed-password", int64(10), int64(20), int64(0))

	mock.ExpectQuery(sqlPattern(GetUserQuery)).
		WithArgs(int64(1001), 0).
		WillReturnRows(rows)

	user, err := store.GetUser(context.Background(), 1001)
	require.NoError(t, err)
	require.Equal(t, int64(1001), user.UserID)
	require.Equal(t, "user@example.com", user.Email)
	require.Equal(t, "hashed-password", user.HashedPassword)
}

func TestCheckEmailAvailability(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"available"}).AddRow(true)

	mock.ExpectQuery(sqlPattern(CheckEmailAvailabilityQuery)).
		WithArgs("user@example.com", 0).
		WillReturnRows(rows)

	available, err := store.CheckEmailAvailability(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.True(t, available)
}

func TestUpdateUserPassword(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(sqlPattern(UpdateUserPasswordStatement)).
		WithArgs("new-hash", sqlmock.AnyArg(), int64(1001), 0).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, store.UpdateUserPassword(context.Background(), 1001, "new-hash"))
}

func TestUpdateUserPasswordNoRows(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(sqlPattern(UpdateUserPasswordStatement)).
		WithArgs("new-hash", sqlmock.AnyArg(), int64(1001), 0).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := store.UpdateUserPassword(context.Background(), 1001, "new-hash")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestUpdateUserEmail(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"user_id", "email", "hashed_password", "created_at", "updated_at", "deleted_at"}).
		AddRow(int64(1001), "new@example.com", "hashed-password", int64(10), int64(30), int64(0))

	mock.ExpectQuery(sqlPattern(UpdateUserEmailQuery)).
		WithArgs("new@example.com", sqlmock.AnyArg(), int64(1001), 0).
		WillReturnRows(rows)

	user, err := store.UpdateUserEmail(context.Background(), 1001, "new@example.com")
	require.NoError(t, err)
	require.Equal(t, int64(1001), user.UserID)
	require.Equal(t, "new@example.com", user.Email)
	require.Equal(t, int64(30), user.UpdatedAt)
}

func TestCreateUserProfile(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(createUserProfileExecPattern()).
		WithArgs(int64(1001), "display name", "avatar://1", sqlmock.AnyArg(), int64(0), int64(0)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	profile, err := store.CreateUserProfile(context.Background(), 1001, "display name", "avatar://1")
	require.NoError(t, err)
	require.Equal(t, int64(1001), profile.UserID)
	require.Equal(t, "display name", profile.Name)
	require.Equal(t, "avatar://1", profile.AvatarURI)
}

func TestGetUserProfile(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"user_id", "name", "avatar_uri", "created_at", "updated_at", "deleted_at"}).
		AddRow(int64(1001), "display name", "avatar://1", int64(10), int64(20), int64(0))

	mock.ExpectQuery(sqlPattern(GetUserProfileQuery)).
		WithArgs(int64(1001), 0).
		WillReturnRows(rows)

	profile, err := store.GetUserProfile(context.Background(), 1001)
	require.NoError(t, err)
	require.Equal(t, int64(1001), profile.UserID)
	require.Equal(t, "display name", profile.Name)
	require.Equal(t, "avatar://1", profile.AvatarURI)
}

func TestUpdateUserProfile(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"user_id", "name", "avatar_uri", "created_at", "updated_at", "deleted_at"}).
		AddRow(int64(1001), "new name", "avatar://2", int64(10), int64(30), int64(0))

	mock.ExpectQuery(sqlPattern(UpdateUserProfileQuery)).
		WithArgs("new name", "avatar://2", sqlmock.AnyArg(), int64(1001), 0).
		WillReturnRows(rows)

	profile, err := store.UpdateUserProfile(context.Background(), 1001, "new name", "avatar://2")
	require.NoError(t, err)
	require.Equal(t, "new name", profile.Name)
	require.Equal(t, "avatar://2", profile.AvatarURI)
	require.Equal(t, int64(30), profile.UpdatedAt)
}

func TestTransactCommit(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectExec(createUserProfileExecPattern()).
		WithArgs(int64(1001), "display name", "", sqlmock.AnyArg(), int64(0), int64(0)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := store.Transact(context.Background(), func(txStore Store) error {
		_, err := txStore.CreateUserProfile(context.Background(), 1001, "display name", "")
		return err
	})
	require.NoError(t, err)
}

func TestTransactRollback(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	errRollback := errors.New("rollback")

	mock.ExpectBegin()
	mock.ExpectRollback()

	err := store.Transact(context.Background(), func(txStore Store) error {
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
}
