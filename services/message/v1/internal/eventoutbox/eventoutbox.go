// Package eventoutbox holds Message service outbox table names and stream key
// conventions.
package eventoutbox

import (
	"fmt"

	"github.com/soasurs/cordis/pkg/outbox"
)

const (
	// MessageStreams and MessageEvents back message.created/updated/deleted
	// and dm.channel.created events.
	MessageStreams = "message_event_streams"
	MessageEvents  = "message_outbox_events"

	// ReadStateStreams and ReadStateEvents back message.read.updated events.
	ReadStateStreams = "read_state_event_streams"
	ReadStateEvents  = "read_state_outbox_events"

	// MessageNotifyChannel and ReadStateNotifyChannel are the PostgreSQL
	// channels used for commit-after wakeups.
	MessageNotifyChannel       = "cordis_message_outbox"
	ReadStateNotifyChannel     = "cordis_read_state_outbox"
	MessageAdvisoryNamespace   = "cordis.message.outbox"
	ReadStateAdvisoryNamespace = "cordis.read-state.outbox"
)

// MessageTables is the outbox table pair for message events.
func MessageTables() outbox.Tables {
	return outbox.Tables{Streams: MessageStreams, Events: MessageEvents}
}

// ReadStateTables is the outbox table pair for read-state events.
func ReadStateTables() outbox.Tables {
	return outbox.Tables{Streams: ReadStateStreams, Events: ReadStateEvents}
}

// MessageStreamKey returns the stream key for a message channel.
func MessageStreamKey(channelID int64) string {
	return fmt.Sprintf("%d", channelID)
}

// ReadStateStreamKey returns the stream key for one user's read state in one
// channel.
func ReadStateStreamKey(userID, channelID int64) string {
	return fmt.Sprintf("%d:%d", userID, channelID)
}

// ReadStateKafkaKey returns the composite Kafka key for read-state events.
func ReadStateKafkaKey(userID, channelID int64) []byte {
	return fmt.Appendf(nil, "%d:%d", userID, channelID)
}
