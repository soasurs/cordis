//go:build integration

package store

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/migration"
	usermigrations "github.com/soasurs/cordis/services/user/v1/db/migrations"
)

// TestSQLStoreWithPostgres shares one PostgreSQL container across all
// integration subtests; each subtest works in its own user ID space.
func TestSQLStoreWithPostgres(t *testing.T) {
	postgres := testkit.StartPostgres(t)
	db, err := database.NewPostgres(database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, migration.Apply(t.Context(), db, usermigrations.Files))

	store := New(db)
	t.Run("users", func(t *testing.T) { testUsers(t, store) })
	t.Run("profiles", func(t *testing.T) { testUserProfiles(t, store) })
	t.Run("transact", func(t *testing.T) { testTransact(t, store) })
	t.Run("constraint enforcement", func(t *testing.T) { testConstraintEnforcement(t, store) })
}

func testUsers(t *testing.T, store Store) {
	const userID = int64(1001)
	ctx := t.Context()

	created, err := store.CreateUser(ctx, userID, "alice@example.com", "hash-a")
	require.NoError(t, err)
	require.Equal(t, userID, created.UserID)
	require.True(t, created.CreatedAt > 0)

	loaded, err := store.GetUser(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, "alice@example.com", loaded.Email)
	require.Equal(t, "hash-a", loaded.HashedPassword)
	_, err = store.GetUser(ctx, 9999)
	require.ErrorIs(t, err, sql.ErrNoRows)

	byEmail, err := store.GetUserWithEmail(ctx, "alice@example.com")
	require.NoError(t, err)
	require.Equal(t, userID, byEmail.UserID)
	_, err = store.GetUserWithEmail(ctx, "missing@example.com")
	require.ErrorIs(t, err, sql.ErrNoRows)

	available, err := store.CheckEmailAvailability(ctx, "alice@example.com")
	require.NoError(t, err)
	require.False(t, available)
	available, err = store.CheckEmailAvailability(ctx, "new@example.com")
	require.NoError(t, err)
	require.True(t, available)

	require.NoError(t, store.UpdateUserPassword(ctx, userID, "hash-b"))
	loaded, err = store.GetUser(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, "hash-b", loaded.HashedPassword)
	require.True(t, loaded.UpdatedAt > 0)
	require.ErrorIs(t, store.UpdateUserPassword(ctx, 9999, "hash"), sql.ErrNoRows)

	updated, err := store.UpdateUserEmail(ctx, userID, "alice2@example.com")
	require.NoError(t, err)
	require.Equal(t, "alice2@example.com", updated.Email)
	require.True(t, updated.UpdatedAt > 0)
	_, err = store.UpdateUserEmail(ctx, 9999, "nobody@example.com")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func testUserProfiles(t *testing.T, store Store) {
	const userID = int64(2001)
	ctx := t.Context()

	profile, err := store.CreateUserProfile(ctx, userID, "Alice", "avatar://a")
	require.NoError(t, err)
	require.Equal(t, userID, profile.UserID)
	require.Equal(t, "Alice", profile.Name)
	require.True(t, profile.CreatedAt > 0)

	loaded, err := store.GetUserProfile(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, "Alice", loaded.Name)
	require.Equal(t, "avatar://a", loaded.AvatarURI)
	_, err = store.GetUserProfile(ctx, 9999)
	require.ErrorIs(t, err, sql.ErrNoRows)

	updated, err := store.UpdateUserProfile(ctx, userID, "Alice Cooper", "avatar://b")
	require.NoError(t, err)
	require.Equal(t, "Alice Cooper", updated.Name)
	require.Equal(t, "avatar://b", updated.AvatarURI)
	require.True(t, updated.UpdatedAt > 0)
	_, err = store.UpdateUserProfile(ctx, 9999, "Nobody", "")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func testTransact(t *testing.T, store Store) {
	const userID = int64(3001)
	ctx := t.Context()

	require.NoError(t, store.Transact(ctx, func(tx Store) error {
		if _, err := tx.CreateUser(ctx, userID, "tx@example.com", "hash"); err != nil {
			return err
		}
		_, err := tx.CreateUserProfile(ctx, userID, "Tx", "")
		return err
	}))
	profile, err := store.GetUserProfile(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, "Tx", profile.Name)

	err = store.Transact(ctx, func(tx Store) error {
		if _, err := tx.UpdateUserEmail(ctx, userID, "rollback@example.com"); err != nil {
			return err
		}
		return errors.New("force rollback")
	})
	require.Error(t, err)
	_, err = store.GetUserWithEmail(ctx, "rollback@example.com")
	require.ErrorIs(t, err, sql.ErrNoRows)
	loaded, err := store.GetUser(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, "tx@example.com", loaded.Email)
}

func testConstraintEnforcement(t *testing.T, store Store) {
	const userID = int64(4001)
	ctx := t.Context()

	_, err := store.CreateUser(ctx, userID, "dup@example.com", "hash")
	require.NoError(t, err)

	_, err = store.CreateUser(ctx, userID, "other@example.com", "hash")
	requireUniqueViolation(t, err)

	_, err = store.CreateUser(ctx, 4002, "dup@example.com", "hash")
	requireUniqueViolation(t, err)
}

func requireUniqueViolation(t *testing.T, err error) {
	t.Helper()
	var pqErr *pq.Error
	require.True(t, errors.As(err, &pqErr), "expected pq.Error, got %v", err)
	require.Equal(t, pq.ErrorCode("23505"), pqErr.Code)
}
