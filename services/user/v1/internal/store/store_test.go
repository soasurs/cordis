package store

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
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
		users (user_id, email, created_at, updated_at, deleted_at)
	VALUES
		($1, $2, $3, $4, $5);
	`)
}

func createUserProfileExecPattern() string {
	return sqlPattern(`
	INSERT INTO
		user_profiles (user_id, username, name, avatar_asset_id, created_at, updated_at, deleted_at)
	VALUES
		($1, $2, $3, $4, $5, $6, $7);
	`)
}

func TestLockRelationshipPairOrdersUserLocks(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(sqlPattern(LockRelationshipUserStatement)).
		WithArgs(int64(1001)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(sqlPattern(LockRelationshipUserStatement)).
		WithArgs(int64(2002)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, store.LockRelationshipPair(context.Background(), 2002, 1001))
}

func TestCreateUser(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectExec(createUserExecPattern()).
		WithArgs(int64(1001), "user@example.com", sqlmock.AnyArg(), int64(0), int64(0)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	user, err := store.CreateUser(context.Background(), 1001, "user@example.com")
	require.NoError(t, err)
	require.Equal(t, int64(1001), user.UserID)
	require.Equal(t, "user@example.com", user.Email)
	require.NotZero(t, user.CreatedAt)
	require.Zero(t, user.UpdatedAt)
	require.Zero(t, user.DeletedAt)
}

func TestGetUser(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"user_id", "email", "created_at", "updated_at", "deleted_at", "email_verified_at"}).
		AddRow(int64(1001), "user@example.com", int64(10), int64(20), int64(0), int64(0))

	mock.ExpectQuery(sqlPattern(GetUserQuery)).
		WithArgs(int64(1001), 0).
		WillReturnRows(rows)

	user, err := store.GetUser(context.Background(), 1001)
	require.NoError(t, err)
	require.Equal(t, int64(1001), user.UserID)
	require.Equal(t, "user@example.com", user.Email)
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

func TestUpdateUserEmail(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"user_id", "email", "created_at", "updated_at", "deleted_at", "email_verified_at"}).
		AddRow(int64(1001), "new@example.com", int64(10), int64(30), int64(0), int64(0))

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
		WithArgs(int64(1001), "alice", "display name", int64(0), sqlmock.AnyArg(), int64(0), int64(0)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	profile, err := store.CreateUserProfile(context.Background(), 1001, "alice", "display name")
	require.NoError(t, err)
	require.Equal(t, int64(1001), profile.UserID)
	require.Equal(t, "alice", profile.Username)
	require.Equal(t, "display name", profile.Name)
	require.Zero(t, profile.AvatarAssetID)
}

func TestGetUserProfile(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"user_id", "username", "name", "avatar_asset_id", "created_at", "updated_at", "deleted_at"}).
		AddRow(int64(1001), "alice", "display name", int64(77), int64(10), int64(20), int64(0))

	mock.ExpectQuery(sqlPattern(GetUserProfileQuery)).
		WithArgs(int64(1001), 0).
		WillReturnRows(rows)

	profile, err := store.GetUserProfile(context.Background(), 1001)
	require.NoError(t, err)
	require.Equal(t, int64(1001), profile.UserID)
	require.Equal(t, "display name", profile.Name)
	require.Equal(t, int64(77), profile.AvatarAssetID)
}

func TestListUserProfiles(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"user_id", "username", "name", "avatar_asset_id", "created_at", "updated_at", "deleted_at"}).
		AddRow(int64(1001), "alice", "Alice", int64(77), int64(10), int64(20), int64(0)).
		AddRow(int64(1002), "bob", "Bob", int64(88), int64(11), int64(21), int64(0))
	userIDs := []int64{1002, 1001}
	mock.ExpectQuery(sqlPattern(ListUserProfilesQuery)).
		WithArgs(pq.Array(userIDs), 0).
		WillReturnRows(rows)

	profiles, err := store.ListUserProfiles(t.Context(), userIDs)
	require.NoError(t, err)
	require.Len(t, profiles, 2)
	require.Equal(t, int64(1001), profiles[0].UserID)
	require.Equal(t, int64(1002), profiles[1].UserID)
}

func TestUpdateUserProfile(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"user_id", "username", "name", "avatar_asset_id", "created_at", "updated_at", "deleted_at"}).
		AddRow(int64(1001), "alice", "new name", int64(77), int64(10), int64(30), int64(0))

	mock.ExpectQuery(sqlPattern(UpdateUserProfileQuery)).
		WithArgs(true, "new name", sqlmock.AnyArg(), int64(1001), 0).
		WillReturnRows(rows)

	name := "new name"
	profile, err := store.UpdateUserProfile(context.Background(), UpdateUserProfileParams{UserID: 1001, Name: &name})
	require.NoError(t, err)
	require.Equal(t, "new name", profile.Name)
	require.Equal(t, int64(77), profile.AvatarAssetID)
	require.Equal(t, int64(30), profile.UpdatedAt)
}

func TestTransactCommit(t *testing.T) {
	store, mock, cleanup := newTestStore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectExec(createUserProfileExecPattern()).
		WithArgs(int64(1001), "alice", "display name", int64(0), sqlmock.AnyArg(), int64(0), int64(0)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := store.Transact(context.Background(), func(txStore Store) error {
		_, err := txStore.CreateUserProfile(context.Background(), 1001, "alice", "display name")
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
