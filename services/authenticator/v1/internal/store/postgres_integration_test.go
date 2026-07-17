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
	authmigrations "github.com/soasurs/cordis/services/authenticator/v1/db/migrations"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
)

// TestSQLStoreWithPostgres shares one PostgreSQL container across all
// integration subtests; each subtest works in its own user/session ID space.
func TestSQLStoreWithPostgres(t *testing.T) {
	postgres := testkit.StartPostgres(t)
	db, err := database.NewPostgres(database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, migration.Apply(t.Context(), db, authmigrations.Files))

	store := New(db)
	t.Run("session lifecycle", func(t *testing.T) { testSessionLifecycle(t, store) })
	t.Run("session revocation", func(t *testing.T) { testSessionRevocation(t, store) })
	t.Run("totp enrollment", func(t *testing.T) { testTOTPEnrollment(t, store) })
	t.Run("totp factor", func(t *testing.T) { testTOTPFactor(t, store) })
	t.Run("login challenge", func(t *testing.T) { testTwoFactorLoginChallenge(t, store) })
	t.Run("recovery codes", func(t *testing.T) { testRecoveryCodes(t, store) })
	t.Run("transact rollback", func(t *testing.T) { testTransactRollback(t, store) })
	t.Run("constraint enforcement", func(t *testing.T) { testConstraintEnforcement(t, store) })
	t.Run("password reset tokens", func(t *testing.T) { testPasswordResetTokens(t, store) })
	t.Run("email verification tokens", func(t *testing.T) { testEmailVerificationTokens(t, store) })
	t.Run("user credentials", func(t *testing.T) { testUserCredentials(t, store) })
}

func testSessionLifecycle(t *testing.T, store Store) {
	const userID = int64(2001)
	ctx := t.Context()
	expiresAt := time.Now().Add(time.Hour).UnixMilli()

	created, err := store.CreateSession(ctx, 1001, userID, "refresh-a", "Cordis", "127.0.0.1", expiresAt)
	require.NoError(t, err)
	require.Equal(t, userID, created.UserID)
	require.True(t, created.CreatedAt > 0)

	loaded, err := store.GetSession(ctx, created.SessionID)
	require.NoError(t, err)
	require.Equal(t, "refresh-a", loaded.RefreshTokenHash)
	require.Equal(t, "Cordis", loaded.UserAgent)
	_, err = store.GetSession(ctx, 9999)
	require.ErrorIs(t, err, sql.ErrNoRows)

	require.ErrorIs(t, store.RotateRefreshToken(ctx, created.SessionID, "wrong-old", "refresh-b"), sql.ErrNoRows)
	require.NoError(t, store.RotateRefreshToken(ctx, created.SessionID, "refresh-a", "refresh-b"))
	loaded, err = store.GetSession(ctx, created.SessionID)
	require.NoError(t, err)
	require.Equal(t, "refresh-b", loaded.RefreshTokenHash)
	require.True(t, loaded.UpdatedAt > 0)

	_, err = store.CreateSession(ctx, 1002, userID, "refresh-c", "", "", expiresAt)
	require.NoError(t, err)
	_, err = store.CreateSession(ctx, 1003, userID, "refresh-expired", "", "", time.Now().Add(-time.Hour).UnixMilli())
	require.NoError(t, err)

	sessions, err := store.ListSessions(ctx, userID)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{1001, 1002}, sessionIDs(sessions))
}

func testSessionRevocation(t *testing.T, store Store) {
	const userID = int64(3001)
	ctx := t.Context()
	expiresAt := time.Now().Add(time.Hour).UnixMilli()
	for _, sessionID := range []int64{2001, 2002, 2003} {
		_, err := store.CreateSession(ctx, sessionID, userID, "revoke-refresh-"+string(rune('a'+sessionID%10)), "", "", expiresAt)
		require.NoError(t, err)
	}

	require.NoError(t, store.RevokeSession(ctx, 2001))
	require.ErrorIs(t, store.RevokeSession(ctx, 2001), sql.ErrNoRows)
	loaded, err := store.GetSession(ctx, 2001)
	require.NoError(t, err)
	require.True(t, loaded.RevokedAt > 0)

	require.ErrorIs(t, store.RevokeUserSession(ctx, 9999, 2002), sql.ErrNoRows)
	require.NoError(t, store.RevokeUserSession(ctx, userID, 2002))
	sessions, err := store.ListSessions(ctx, userID)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{2003}, sessionIDs(sessions))

	const currentSessionID = int64(2004)
	_, err = store.CreateSession(ctx, currentSessionID, userID, "revoke-refresh-current", "", "", expiresAt)
	require.NoError(t, err)
	affected, err := store.RevokeOtherSessions(ctx, userID, currentSessionID)
	require.NoError(t, err)
	require.Equal(t, int64(1), affected)
	sessions, err = store.ListSessions(ctx, userID)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{currentSessionID}, sessionIDs(sessions))
}

func testTOTPEnrollment(t *testing.T, store Store) {
	const userID = int64(4001)
	ctx := t.Context()
	now := time.Now().UnixMilli()

	_, err := store.GetTOTPEnrollment(ctx, userID, "enroll-a", false)
	require.ErrorIs(t, err, sql.ErrNoRows)

	require.NoError(t, store.CreateTOTPEnrollment(ctx, &model.TOTPEnrollment{
		UserID: userID, TokenHash: "enroll-a", SecretCiphertext: []byte("secret-a"),
		EncryptionKeyID: "key-1", CreatedAt: now, ExpiresAt: now + 600_000,
	}))

	err = store.CreateTOTPEnrollment(ctx, &model.TOTPEnrollment{
		UserID: userID, TokenHash: "enroll-b", SecretCiphertext: []byte("secret-b"),
		EncryptionKeyID: "key-1", CreatedAt: now, ExpiresAt: now + 600_000,
	})
	require.ErrorIs(t, err, sql.ErrNoRows)

	loaded, err := store.GetTOTPEnrollment(ctx, userID, "enroll-a", true)
	require.NoError(t, err)
	require.Equal(t, []byte("secret-a"), loaded.SecretCiphertext)
	require.Equal(t, "key-1", loaded.EncryptionKeyID)

	const expiredUserID = int64(4002)
	require.NoError(t, store.CreateTOTPEnrollment(ctx, &model.TOTPEnrollment{
		UserID: expiredUserID, TokenHash: "enroll-old", SecretCiphertext: []byte("old"),
		EncryptionKeyID: "key-1", CreatedAt: now - 700_000, ExpiresAt: now - 100_000,
	}))
	require.NoError(t, store.CreateTOTPEnrollment(ctx, &model.TOTPEnrollment{
		UserID: expiredUserID, TokenHash: "enroll-new", SecretCiphertext: []byte("new"),
		EncryptionKeyID: "key-1", CreatedAt: now, ExpiresAt: now + 600_000,
	}))
	replaced, err := store.GetTOTPEnrollment(ctx, expiredUserID, "enroll-new", false)
	require.NoError(t, err)
	require.Equal(t, []byte("new"), replaced.SecretCiphertext)

	require.ErrorIs(t, store.DeleteTOTPEnrollment(ctx, userID, "enroll-missing"), sql.ErrNoRows)
	require.NoError(t, store.DeleteTOTPEnrollment(ctx, userID, "enroll-a"))
	_, err = store.GetTOTPEnrollment(ctx, userID, "enroll-a", false)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func testTOTPFactor(t *testing.T, store Store) {
	const userID = int64(5001)
	ctx := t.Context()
	now := time.Now().UnixMilli()

	_, err := store.GetTOTPFactor(ctx, userID, false)
	require.ErrorIs(t, err, sql.ErrNoRows)

	require.NoError(t, store.UpsertTOTPFactor(ctx, &model.TOTPFactor{
		UserID: userID, SecretCiphertext: []byte("secret-1"), EncryptionKeyID: "key-1",
		LastUsedCounter: -1, EnabledAt: now, CreatedAt: now,
	}))
	factor, err := store.GetTOTPFactor(ctx, userID, false)
	require.NoError(t, err)
	require.Equal(t, []byte("secret-1"), factor.SecretCiphertext)
	require.Equal(t, int64(-1), factor.LastUsedCounter)

	require.NoError(t, store.UpsertTOTPFactor(ctx, &model.TOTPFactor{
		UserID: userID, SecretCiphertext: []byte("secret-2"), EncryptionKeyID: "key-2",
		LastUsedCounter: -1, EnabledAt: now, CreatedAt: now, UpdatedAt: now,
	}))
	factor, err = store.GetTOTPFactor(ctx, userID, true)
	require.NoError(t, err)
	require.Equal(t, []byte("secret-2"), factor.SecretCiphertext)
	require.Equal(t, "key-2", factor.EncryptionKeyID)

	require.NoError(t, store.UpdateTOTPLastUsedCounter(ctx, userID, 100))
	require.ErrorIs(t, store.UpdateTOTPLastUsedCounter(ctx, userID, 100), sql.ErrNoRows)
	require.ErrorIs(t, store.UpdateTOTPLastUsedCounter(ctx, userID, 99), sql.ErrNoRows)
	require.NoError(t, store.UpdateTOTPLastUsedCounter(ctx, userID, 101))
	factor, err = store.GetTOTPFactor(ctx, userID, false)
	require.NoError(t, err)
	require.Equal(t, int64(101), factor.LastUsedCounter)

	require.ErrorIs(t, store.DeleteTOTPFactor(ctx, 9999), sql.ErrNoRows)
	require.NoError(t, store.DeleteTOTPFactor(ctx, userID))
	_, err = store.GetTOTPFactor(ctx, userID, false)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func testTwoFactorLoginChallenge(t *testing.T, store Store) {
	const userID = int64(6001)
	ctx := t.Context()
	now := time.Now().UnixMilli()

	_, err := store.GetTwoFactorLoginChallenge(ctx, "challenge-missing", false)
	require.ErrorIs(t, err, sql.ErrNoRows)

	require.NoError(t, store.CreateTwoFactorLoginChallenge(ctx, &model.TwoFactorLoginChallenge{
		TokenHash: "challenge-a", UserID: userID, UserAgent: "Cordis", IP: "127.0.0.1",
		CreatedAt: now, ExpiresAt: now + 300_000,
	}))
	challenge, err := store.GetTwoFactorLoginChallenge(ctx, "challenge-a", true)
	require.NoError(t, err)
	require.Equal(t, userID, challenge.UserID)
	require.Equal(t, 0, challenge.Attempts)

	require.NoError(t, store.IncrementTwoFactorLoginChallengeAttempts(ctx, "challenge-a"))
	require.NoError(t, store.IncrementTwoFactorLoginChallengeAttempts(ctx, "challenge-a"))
	challenge, err = store.GetTwoFactorLoginChallenge(ctx, "challenge-a", false)
	require.NoError(t, err)
	require.Equal(t, 2, challenge.Attempts)

	require.NoError(t, store.ConsumeTwoFactorLoginChallenge(ctx, "challenge-a"))
	require.ErrorIs(t, store.ConsumeTwoFactorLoginChallenge(ctx, "challenge-a"), sql.ErrNoRows)
	require.ErrorIs(t, store.IncrementTwoFactorLoginChallengeAttempts(ctx, "challenge-a"), sql.ErrNoRows)
	challenge, err = store.GetTwoFactorLoginChallenge(ctx, "challenge-a", false)
	require.NoError(t, err)
	require.True(t, challenge.ConsumedAt > 0)
}

func testRecoveryCodes(t *testing.T, store Store) {
	const userID = int64(7001)
	ctx := t.Context()

	count, err := store.CountUnusedRecoveryCodes(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, int64(0), count)

	require.NoError(t, store.Transact(ctx, func(tx Store) error {
		return tx.ReplaceRecoveryCodes(ctx, userID, []string{"code-1", "code-2", "code-3"})
	}))
	count, err = store.CountUnusedRecoveryCodes(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, int64(3), count)

	require.NoError(t, store.ConsumeRecoveryCode(ctx, userID, "code-2"))
	require.ErrorIs(t, store.ConsumeRecoveryCode(ctx, userID, "code-2"), sql.ErrNoRows)
	require.ErrorIs(t, store.ConsumeRecoveryCode(ctx, userID, "code-missing"), sql.ErrNoRows)
	count, err = store.CountUnusedRecoveryCodes(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, int64(2), count)

	require.NoError(t, store.Transact(ctx, func(tx Store) error {
		return tx.ReplaceRecoveryCodes(ctx, userID, []string{"code-4"})
	}))
	count, err = store.CountUnusedRecoveryCodes(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	require.NoError(t, store.ConsumeRecoveryCode(ctx, userID, "code-4"))
}

func testTransactRollback(t *testing.T, store Store) {
	const userID = int64(8001)
	ctx := t.Context()
	expiresAt := time.Now().Add(time.Hour).UnixMilli()

	err := store.Transact(ctx, func(tx Store) error {
		if _, err := tx.CreateSession(ctx, 8101, userID, "rollback-refresh", "", "", expiresAt); err != nil {
			return err
		}
		return errors.New("force rollback")
	})
	require.Error(t, err)
	_, err = store.GetSession(ctx, 8101)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func testConstraintEnforcement(t *testing.T, store Store) {
	const userID = int64(9001)
	ctx := t.Context()
	now := time.Now().UnixMilli()
	expiresAt := time.Now().Add(time.Hour).UnixMilli()

	_, err := store.CreateSession(ctx, 9101, userID, "unique-refresh", "", "", expiresAt)
	require.NoError(t, err)
	_, err = store.CreateSession(ctx, 9102, userID, "unique-refresh", "", "", expiresAt)
	requireUniqueViolation(t, err)

	require.NoError(t, store.CreateTwoFactorLoginChallenge(ctx, &model.TwoFactorLoginChallenge{
		TokenHash: "unique-challenge", UserID: userID, CreatedAt: now, ExpiresAt: now + 300_000,
	}))
	err = store.CreateTwoFactorLoginChallenge(ctx, &model.TwoFactorLoginChallenge{
		TokenHash: "unique-challenge", UserID: userID, CreatedAt: now, ExpiresAt: now + 300_000,
	})
	requireUniqueViolation(t, err)
}

func sessionIDs(sessions []*model.Session) []int64 {
	out := make([]int64, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, session.SessionID)
	}
	return out
}

func requireUniqueViolation(t *testing.T, err error) {
	t.Helper()
	var pqErr *pq.Error
	require.True(t, errors.As(err, &pqErr), "expected pq.Error, got %v", err)
	require.Equal(t, pq.ErrorCode("23505"), pqErr.Code)
}

func testPasswordResetTokens(t *testing.T, store Store) {
	const userID = int64(9001)
	ctx := t.Context()
	now := time.Now().UnixMilli()

	require.NoError(t, store.UpsertPasswordResetToken(ctx, &model.PasswordResetToken{
		UserID: userID, TokenHash: "reset-hash-a", CreatedAt: now, ExpiresAt: now + 60_000,
	}))

	loaded, err := store.GetPasswordResetToken(ctx, "reset-hash-a", false)
	require.NoError(t, err)
	require.Equal(t, userID, loaded.UserID)
	require.Zero(t, loaded.ConsumedAt)

	// A newer request replaces the previous token for the same user.
	require.NoError(t, store.UpsertPasswordResetToken(ctx, &model.PasswordResetToken{
		UserID: userID, TokenHash: "reset-hash-b", CreatedAt: now + 1, ExpiresAt: now + 120_000,
	}))
	_, err = store.GetPasswordResetToken(ctx, "reset-hash-a", false)
	require.ErrorIs(t, err, sql.ErrNoRows)

	require.NoError(t, store.ConsumePasswordResetToken(ctx, "reset-hash-b", now+2))
	loaded, err = store.GetPasswordResetToken(ctx, "reset-hash-b", true)
	require.NoError(t, err)
	require.Equal(t, now+2, loaded.ConsumedAt)
	require.ErrorIs(t, store.ConsumePasswordResetToken(ctx, "reset-hash-b", now+3), sql.ErrNoRows)
	require.ErrorIs(t, store.ConsumePasswordResetToken(ctx, "reset-hash-missing", now+3), sql.ErrNoRows)

	// Re-requesting after consumption reactivates the row.
	require.NoError(t, store.UpsertPasswordResetToken(ctx, &model.PasswordResetToken{
		UserID: userID, TokenHash: "reset-hash-c", CreatedAt: now + 4, ExpiresAt: now + 180_000,
	}))
	loaded, err = store.GetPasswordResetToken(ctx, "reset-hash-c", false)
	require.NoError(t, err)
	require.Zero(t, loaded.ConsumedAt)
}

func testEmailVerificationTokens(t *testing.T, store Store) {
	const userID = int64(9101)
	ctx := t.Context()
	now := time.Now().UnixMilli()

	require.NoError(t, store.UpsertEmailVerificationToken(ctx, &model.EmailVerificationToken{
		UserID: userID, TokenHash: "verify-hash-a", Email: "a@example.com",
		CreatedAt: now, ExpiresAt: now + 60_000,
	}))

	loaded, err := store.GetEmailVerificationToken(ctx, "verify-hash-a", false)
	require.NoError(t, err)
	require.Equal(t, "a@example.com", loaded.Email)

	require.NoError(t, store.UpsertEmailVerificationToken(ctx, &model.EmailVerificationToken{
		UserID: userID, TokenHash: "verify-hash-b", Email: "b@example.com",
		CreatedAt: now + 1, ExpiresAt: now + 120_000,
	}))
	_, err = store.GetEmailVerificationToken(ctx, "verify-hash-a", false)
	require.ErrorIs(t, err, sql.ErrNoRows)

	require.NoError(t, store.ConsumeEmailVerificationToken(ctx, "verify-hash-b", now+2))
	loaded, err = store.GetEmailVerificationToken(ctx, "verify-hash-b", true)
	require.NoError(t, err)
	require.Equal(t, now+2, loaded.ConsumedAt)
	require.ErrorIs(t, store.ConsumeEmailVerificationToken(ctx, "verify-hash-b", now+3), sql.ErrNoRows)
}

func testUserCredentials(t *testing.T, store Store) {
	const userID = int64(9201)
	ctx := t.Context()
	now := time.Now().UnixMilli()

	require.NoError(t, store.CreateUserCredential(ctx, &model.UserCredential{
		UserID: userID, HashedPassword: "hash-a", CreatedAt: now,
	}))

	// Insert-if-absent: a second create loses the race and reports it.
	err := store.CreateUserCredential(ctx, &model.UserCredential{
		UserID: userID, HashedPassword: "hash-b", CreatedAt: now + 1,
	})
	require.ErrorIs(t, err, sql.ErrNoRows)

	loaded, err := store.GetUserCredential(ctx, userID, false)
	require.NoError(t, err)
	require.Equal(t, "hash-a", loaded.HashedPassword)
	require.Zero(t, loaded.UpdatedAt)

	require.NoError(t, store.UpdateUserCredential(ctx, userID, "hash-c", now+2))
	loaded, err = store.GetUserCredential(ctx, userID, true)
	require.NoError(t, err)
	require.Equal(t, "hash-c", loaded.HashedPassword)
	require.Equal(t, now+2, loaded.UpdatedAt)
	require.ErrorIs(t, store.UpdateUserCredential(ctx, userID+1, "hash-x", now+3), sql.ErrNoRows)

	// Upsert covers both the replace and the create-on-recovery paths.
	require.NoError(t, store.UpsertUserCredential(ctx, userID, "hash-d", now+4))
	loaded, err = store.GetUserCredential(ctx, userID, false)
	require.NoError(t, err)
	require.Equal(t, "hash-d", loaded.HashedPassword)

	require.NoError(t, store.UpsertUserCredential(ctx, userID+1, "hash-e", now+5))
	loaded, err = store.GetUserCredential(ctx, userID+1, false)
	require.NoError(t, err)
	require.Equal(t, "hash-e", loaded.HashedPassword)
	require.Zero(t, loaded.UpdatedAt)

	_, err = store.GetUserCredential(ctx, userID+99, false)
	require.ErrorIs(t, err, sql.ErrNoRows)
}
