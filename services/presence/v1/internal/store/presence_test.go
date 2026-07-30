package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAggregateUserPresenceUsesUserPreference(t *testing.T) {
	sessions := []UserSession{
		{UserID: 1001, SessionID: "desktop", LastSeenAt: 10},
		{UserID: 1001, SessionID: "mobile", LastSeenAt: 20},
	}

	for _, tt := range []struct {
		name       string
		preference PresenceStatus
		expected   PresenceStatus
	}{
		{name: "online", preference: PresenceStatusOnline, expected: PresenceStatusOnline},
		{name: "idle", preference: PresenceStatusIdle, expected: PresenceStatusIdle},
		{name: "dnd", preference: PresenceStatusDND, expected: PresenceStatusDND},
		{name: "invisible", preference: PresenceStatusInvisible, expected: PresenceStatusOffline},
	} {
		t.Run(tt.name, func(t *testing.T) {
			presence := aggregateUserPresence(1001, UserPresencePreference{
				UserID: 1001, Status: tt.preference, Version: 1,
			}, sessions)

			require.Equal(t, tt.expected, presence.Status)
			require.Equal(t, int64(20), presence.LastSeenAt)
			require.Len(t, presence.Sessions, 2)
		})
	}
}

func TestAggregateUserPresenceIsOfflineWithoutLiveSessions(t *testing.T) {
	presence := aggregateUserPresence(1001, UserPresencePreference{
		UserID: 1001, Status: PresenceStatusDND, Version: 1,
	}, nil)

	require.Equal(t, PresenceStatusOffline, presence.Status)
}
