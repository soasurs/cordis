package token

import (
	"errors"
	"testing"
	"time"
)

func TestIssueAndParseAccessToken(t *testing.T) {
	manager := newTestManager(t)
	now := time.Now()

	issued, err := manager.IssueAccessToken(1001, 2001, now)
	if err != nil {
		t.Fatalf("IssueAccessToken returned error: %v", err)
	}

	parsed, err := manager.ParseAccessToken(issued.Raw)
	if err != nil {
		t.Fatalf("ParseAccessToken returned error: %v", err)
	}
	if parsed.UserID != 1001 || parsed.SessionID != 2001 || parsed.ExpiresAt <= now.UnixMilli() {
		t.Fatalf("unexpected token: %+v", parsed)
	}
}

func TestRefreshTokenDoesNotParseAsAccessToken(t *testing.T) {
	manager := newTestManager(t)
	now := time.Now()

	issued, err := manager.IssueRefreshToken(1001, 2001, now.Add(time.Hour).UnixMilli(), now)
	if err != nil {
		t.Fatalf("IssueRefreshToken returned error: %v", err)
	}

	_, err = manager.ParseAccessToken(issued.Raw)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestAccessTokenDoesNotParseAsRefreshToken(t *testing.T) {
	manager := newTestManager(t)
	now := time.Now()

	issued, err := manager.IssueAccessToken(1001, 2001, now)
	if err != nil {
		t.Fatalf("IssueAccessToken returned error: %v", err)
	}

	_, err = manager.ParseRefreshToken(issued.Raw)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestRefreshTokenExpiresNoLaterThanSession(t *testing.T) {
	manager := newTestManager(t)
	now := time.Now()
	sessionExpiresAt := now.Add(15 * time.Minute).UnixMilli()

	issued, err := manager.IssueRefreshToken(1001, 2001, sessionExpiresAt, now)
	if err != nil {
		t.Fatalf("IssueRefreshToken returned error: %v", err)
	}
	if issued.ExpiresAt > sessionExpiresAt {
		t.Fatalf("expected refresh token to expire no later than session expiry, got %d", issued.ExpiresAt)
	}
}

func TestHash(t *testing.T) {
	first := Hash("token")
	second := Hash("token")
	other := Hash("other")

	if first == "" || first != second || first == other {
		t.Fatalf("unexpected hashes: first=%q second=%q other=%q", first, second, other)
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()

	manager, err := NewManager(Config{
		Issuer:        "cordis.authenticator.v1",
		AccessSecret:  "access-secret-with-at-least-32-bytes",
		RefreshSecret: "refresh-secret-with-at-least-32-bytes",
		AccessTTL:     15 * time.Minute,
		RefreshTTL:    24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	return manager
}

func TestNewManagerRejectsSharedSecret(t *testing.T) {
	_, err := NewManager(Config{
		Issuer:        "cordis.authenticator.v1",
		AccessSecret:  "shared-secret-with-at-least-32-bytes",
		RefreshSecret: "shared-secret-with-at-least-32-bytes",
		AccessTTL:     15 * time.Minute,
		RefreshTTL:    24 * time.Hour,
	})
	if err == nil {
		t.Fatal("expected shared secrets to be rejected")
	}
}
