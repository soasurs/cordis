package token

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIssueAndParseAccessToken(t *testing.T) {
	manager := newTestManager(t)
	now := time.Now()

	issued, err := manager.IssueAccessToken(1001, 2001, now)
	require.NoError(t, err)

	parsed, err := manager.ParseAccessToken(issued.Raw)
	require.NoError(t, err)
	require.Equal(t, int64(1001), parsed.UserID)
	require.Equal(t, int64(2001), parsed.SessionID)
	require.Greater(t, parsed.ExpiresAt, now.UnixMilli())
}

func TestRefreshTokenDoesNotParseAsAccessToken(t *testing.T) {
	manager := newTestManager(t)
	now := time.Now()

	issued, err := manager.IssueRefreshToken(1001, 2001, now.Add(time.Hour).UnixMilli(), now)
	require.NoError(t, err)

	_, err = manager.ParseAccessToken(issued.Raw)
	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestAccessTokenDoesNotParseAsRefreshToken(t *testing.T) {
	manager := newTestManager(t)
	now := time.Now()

	issued, err := manager.IssueAccessToken(1001, 2001, now)
	require.NoError(t, err)

	_, err = manager.ParseRefreshToken(issued.Raw)
	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestRefreshTokenExpiresNoLaterThanSession(t *testing.T) {
	manager := newTestManager(t)
	now := time.Now()
	sessionExpiresAt := now.Add(15 * time.Minute).UnixMilli()

	issued, err := manager.IssueRefreshToken(1001, 2001, sessionExpiresAt, now)
	require.NoError(t, err)
	require.LessOrEqual(t, issued.ExpiresAt, sessionExpiresAt)
}

func TestHash(t *testing.T) {
	first := Hash("token")
	second := Hash("token")
	other := Hash("other")

	require.NotEmpty(t, first)
	require.Equal(t, first, second)
	require.NotEqual(t, first, other)
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
	require.NoError(t, err)
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
	require.Error(t, err)
}
