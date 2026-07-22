//go:build integration

package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/migration"
	usermigrations "github.com/soasurs/cordis/services/user/v1/db/migrations"
	"github.com/soasurs/cordis/services/user/v1/internal/model"
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
	t.Run("email verification", func(t *testing.T) { testEmailVerification(t, store) })
	t.Run("usernames", func(t *testing.T) { testUsernames(t, store) })
	t.Run("relationships", func(t *testing.T) { testRelationships(t, store) })
}

func testUsers(t *testing.T, store Store) {
	const userID = int64(1001)
	ctx := t.Context()

	created, err := store.CreateUser(ctx, userID, "alice@example.com")
	require.NoError(t, err)
	require.Equal(t, userID, created.UserID)
	require.True(t, created.CreatedAt > 0)

	loaded, err := store.GetUser(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, "alice@example.com", loaded.Email)
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

	profile, err := store.CreateUserProfile(ctx, userID, "it_user_2001", "Alice", "avatar://a")
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

	_, err = store.CreateUserProfile(ctx, userID+1, "it_user_2002", "Bob", "avatar://bob")
	require.NoError(t, err)
	profiles, err := store.ListUserProfiles(ctx, []int64{userID + 1, userID, 9999})
	require.NoError(t, err)
	require.Len(t, profiles, 2)
	require.Equal(t, userID, profiles[0].UserID)
	require.Equal(t, userID+1, profiles[1].UserID)

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
		if _, err := tx.CreateUser(ctx, userID, "tx@example.com"); err != nil {
			return err
		}
		_, err := tx.CreateUserProfile(ctx, userID, "it_user_3001", "Tx", "")
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

	_, err := store.CreateUser(ctx, userID, "dup@example.com")
	require.NoError(t, err)

	_, err = store.CreateUser(ctx, userID, "other@example.com")
	requireUniqueViolation(t, err)

	_, err = store.CreateUser(ctx, 4002, "dup@example.com")
	requireUniqueViolation(t, err)
}

func requireUniqueViolation(t *testing.T, err error) {
	t.Helper()
	var pqErr *pq.Error
	require.True(t, errors.As(err, &pqErr), "expected pq.Error, got %v", err)
	require.Equal(t, pq.ErrorCode("23505"), pqErr.Code)
}

func testEmailVerification(t *testing.T, store Store) {
	const userID = int64(5001)
	ctx := t.Context()

	created, err := store.CreateUser(ctx, userID, "verify@example.com")
	require.NoError(t, err)
	require.Zero(t, created.EmailVerifiedAt)

	// Verification only applies while the email still matches.
	err = store.MarkUserEmailVerified(ctx, userID, "other@example.com", 4001)
	require.ErrorIs(t, err, sql.ErrNoRows)

	require.NoError(t, store.MarkUserEmailVerified(ctx, userID, "verify@example.com", 4001))
	loaded, err := store.GetUser(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, int64(4001), loaded.EmailVerifiedAt)

	// Changing the email clears the verification state.
	updated, err := store.UpdateUserEmail(ctx, userID, "changed@example.com")
	require.NoError(t, err)
	require.Zero(t, updated.EmailVerifiedAt)
	loaded, err = store.GetUser(ctx, userID)
	require.NoError(t, err)
	require.Zero(t, loaded.EmailVerifiedAt)

	err = store.MarkUserEmailVerified(ctx, userID+1, "changed@example.com", 4002)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func testUsernames(t *testing.T, store Store) {
	const userID = int64(6001)
	ctx := t.Context()

	_, err := store.CreateUser(ctx, userID, "handle@example.com")
	require.NoError(t, err)
	_, err = store.CreateUserProfile(ctx, userID, "handle_owner", "Handle Owner", "")
	require.NoError(t, err)

	profile, err := store.GetUserProfileByUsername(ctx, "handle_owner")
	require.NoError(t, err)
	require.Equal(t, userID, profile.UserID)
	require.Equal(t, "handle_owner", profile.Username)

	_, err = store.GetUserProfileByUsername(ctx, "missing_handle")
	require.ErrorIs(t, err, sql.ErrNoRows)

	// The active unique index rejects a duplicate handle.
	_, err = store.CreateUser(ctx, userID+1, "handle2@example.com")
	require.NoError(t, err)
	_, err = store.CreateUserProfile(ctx, userID+1, "handle_owner", "Second", "")
	var pqErr *pq.Error
	require.True(t, errors.As(err, &pqErr))
	require.Equal(t, pq.ErrorCode("23505"), pqErr.Code)
	require.Equal(t, "user_profiles_username_active_idx", pqErr.Constraint)

	// The CHECK constraint rejects malformed handles.
	_, err = store.CreateUserProfile(ctx, userID+1, "Bad Handle!", "Second", "")
	require.True(t, errors.As(err, &pqErr))
	require.Equal(t, pq.ErrorCode("23514"), pqErr.Code)

	// Renaming releases the old handle for someone else to claim.
	renamed, err := store.UpdateUsername(ctx, userID, "handle_renamed")
	require.NoError(t, err)
	require.Equal(t, "handle_renamed", renamed.Username)
	_, err = store.CreateUserProfile(ctx, userID+1, "handle_owner", "Second", "")
	require.NoError(t, err)
	_, err = store.UpdateUsername(ctx, userID, "handle_owner")
	require.True(t, errors.As(err, &pqErr))
	require.Equal(t, "user_profiles_username_active_idx", pqErr.Constraint)
	_, err = store.UpdateUsername(ctx, userID+99, "free_handle")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func testRelationships(t *testing.T, store Store) {
	const userA, userB, userC = int64(7001), int64(7002), int64(7003)
	ctx := t.Context()
	now := time.Now().UnixMilli()

	require.NoError(t, store.Transact(ctx, func(txStore Store) error {
		return txStore.LockRelationshipPair(ctx, userB, userA)
	}))

	require.NoError(t, store.UpsertRelationship(ctx, &model.Relationship{UserID: userA, TargetID: userB, Type: model.RelationshipOutgoing, CreatedAt: now}))
	require.NoError(t, store.UpsertRelationship(ctx, &model.Relationship{UserID: userB, TargetID: userA, Type: model.RelationshipIncoming, CreatedAt: now}))
	require.NoError(t, store.UpsertRelationship(ctx, &model.Relationship{UserID: userA, TargetID: userC, Type: model.RelationshipBlocked, CreatedAt: now}))

	loaded, err := store.GetRelationship(ctx, userA, userB)
	require.NoError(t, err)
	require.Equal(t, model.RelationshipOutgoing, loaded.Type)
	require.Zero(t, loaded.UpdatedAt)

	// Upsert replaces the type and stamps updated_at.
	require.NoError(t, store.UpsertRelationship(ctx, &model.Relationship{UserID: userA, TargetID: userB, Type: model.RelationshipFriend, CreatedAt: now + 5}))
	loaded, err = store.GetRelationship(ctx, userA, userB)
	require.NoError(t, err)
	require.Equal(t, model.RelationshipFriend, loaded.Type)
	require.Equal(t, now+5, loaded.UpdatedAt)

	relationships, err := store.ListRelationships(ctx, ListRelationshipsParams{UserID: userA, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, []int64{userC, userB}, idsOfRelationships(relationships))

	relationships, err = store.ListRelationships(ctx, ListRelationshipsParams{UserID: userA, Type: model.RelationshipBlocked, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, []int64{userC}, idsOfRelationships(relationships))

	relationships, err = store.ListRelationships(ctx, ListRelationshipsParams{UserID: userA, BeforeTargetID: userC, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, []int64{userB}, idsOfRelationships(relationships))

	relationships, err = store.ListRelationshipsByTargets(ctx, userA, []int64{userB, userC, 9999})
	require.NoError(t, err)
	require.Len(t, relationships, 2)

	// DeleteRelationshipExceptBlocked never clears a block.
	require.NoError(t, store.DeleteRelationshipExceptBlocked(ctx, userA, userC))
	_, err = store.GetRelationship(ctx, userA, userC)
	require.NoError(t, err)
	require.NoError(t, store.DeleteRelationshipExceptBlocked(ctx, userA, userB))
	_, err = store.GetRelationship(ctx, userA, userB)
	require.ErrorIs(t, err, sql.ErrNoRows)

	require.NoError(t, store.DeleteRelationship(ctx, userA, userC))
	require.ErrorIs(t, store.DeleteRelationship(ctx, userA, userC), sql.ErrNoRows)

	// Constraints: self relationships and unknown types are rejected.
	var pqErr *pq.Error
	err = store.UpsertRelationship(ctx, &model.Relationship{UserID: userA, TargetID: userA, Type: model.RelationshipFriend, CreatedAt: now})
	require.True(t, errors.As(err, &pqErr))
	require.Equal(t, pq.ErrorCode("23514"), pqErr.Code)
	err = store.UpsertRelationship(ctx, &model.Relationship{UserID: userA, TargetID: userB, Type: 9, CreatedAt: now})
	require.True(t, errors.As(err, &pqErr))
	require.Equal(t, pq.ErrorCode("23514"), pqErr.Code)
}

func idsOfRelationships(relationships []*model.Relationship) []int64 {
	ids := make([]int64, 0, len(relationships))
	for _, relationship := range relationships {
		ids = append(ids, relationship.TargetID)
	}
	return ids
}
