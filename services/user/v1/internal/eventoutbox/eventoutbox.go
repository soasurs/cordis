// Package eventoutbox holds User service outbox table names and stream key
// conventions.
package eventoutbox

import (
	"strconv"

	"github.com/soasurs/cordis/pkg/outbox"
)

const (
	// UserStreams and UserEvents back relationship and profile events.
	UserStreams = "user_event_streams"
	UserEvents  = "user_outbox_events"

	// UserNotifyChannel is the PostgreSQL channel used for commit-after
	// wakeups.
	UserNotifyChannel = "cordis_user_outbox"

	// UserAdvisoryNamespace separates advisory locks and NOTIFY listeners
	// for the User outbox relay.
	UserAdvisoryNamespace = "cordis.user.outbox"
)

// Tables returns the outbox table pair for user events.
func Tables() outbox.Tables {
	return outbox.Tables{Streams: UserStreams, Events: UserEvents}
}

// StreamKey returns the per-recipient stream key for a user event.
func StreamKey(userID int64) string {
	return strconv.FormatInt(userID, 10)
}

// KafkaKey returns the Kafka key for a user-routed event.
func KafkaKey(userID int64) []byte {
	return strconv.AppendInt(nil, userID, 10)
}
