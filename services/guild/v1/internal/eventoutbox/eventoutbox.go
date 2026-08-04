// Package eventoutbox holds Guild service outbox table names and stream key
// conventions.
package eventoutbox

import (
	"strconv"

	"github.com/soasurs/cordis/pkg/outbox"
)

const (
	// GuildStreams and GuildEvents back all guild realtime events.
	GuildStreams = "guild_event_streams"
	GuildEvents  = "guild_outbox_events"

	// GuildNotifyChannel is the PostgreSQL channel used for commit-after
	// wakeups.
	GuildNotifyChannel = "cordis_guild_outbox"

	// GuildAdvisoryNamespace separates advisory locks and NOTIFY listeners
	// for the Guild outbox relay.
	GuildAdvisoryNamespace = "cordis.guild.outbox"
)

// Tables returns the outbox table pair for guild events.
func Tables() outbox.Tables {
	return outbox.Tables{Streams: GuildStreams, Events: GuildEvents}
}

// StreamKey returns the per-guild stream key for a guild event.
func StreamKey(guildID int64) string {
	return strconv.FormatInt(guildID, 10)
}

// KafkaKey returns the Kafka key for a guild-routed event.
func KafkaKey(guildID int64) []byte {
	return strconv.AppendInt(nil, guildID, 10)
}
