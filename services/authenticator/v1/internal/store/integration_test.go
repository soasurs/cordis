//go:build integration

package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/soasurs/cordis/internal/testpostgres"
	authenticatormigrations "github.com/soasurs/cordis/services/authenticator/v1/db/migrations"
)

func TestSQLStoreSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	store := New(testpostgres.New(t, authenticatormigrations.Files))
	expiresAt := time.Now().Add(time.Hour).UnixMilli()

	created, err := store.CreateSession(ctx, 2001, 1001, "refresh-hash-1", "test-agent", "127.0.0.1", expiresAt)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if created.SessionID != 2001 ||
		created.UserID != 1001 ||
		created.RefreshTokenHash != "refresh-hash-1" ||
		created.UserAgent != "test-agent" ||
		created.IP != "127.0.0.1" {
		t.Fatalf("unexpected created session: %+v", created)
	}

	session, err := store.GetSession(ctx, 2001)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session.ExpiresAt != expiresAt || session.RevokedAt != 0 {
		t.Fatalf("unexpected session: %+v", session)
	}

	if err := store.RotateRefreshToken(ctx, 2001, "wrong-hash", "refresh-hash-2"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("rotate with wrong hash error = %v, want %v", err, sql.ErrNoRows)
	}
	if err := store.RotateRefreshToken(ctx, 2001, "refresh-hash-1", "refresh-hash-2"); err != nil {
		t.Fatalf("rotate refresh token: %v", err)
	}

	rotated, err := store.GetSession(ctx, 2001)
	if err != nil {
		t.Fatalf("get rotated session: %v", err)
	}
	if rotated.RefreshTokenHash != "refresh-hash-2" || rotated.UpdatedAt == 0 {
		t.Fatalf("unexpected rotated session: %+v", rotated)
	}

	if err := store.RevokeSession(ctx, 2001); err != nil {
		t.Fatalf("revoke session: %v", err)
	}

	revoked, err := store.GetSession(ctx, 2001)
	if err != nil {
		t.Fatalf("get revoked session: %v", err)
	}
	if revoked.RevokedAt == 0 || revoked.UpdatedAt != revoked.RevokedAt {
		t.Fatalf("unexpected revoked session: %+v", revoked)
	}

	if err := store.RevokeSession(ctx, 2001); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("second revoke error = %v, want %v", err, sql.ErrNoRows)
	}
	if err := store.RotateRefreshToken(ctx, 2001, "refresh-hash-2", "refresh-hash-3"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("rotate revoked session error = %v, want %v", err, sql.ErrNoRows)
	}
}
