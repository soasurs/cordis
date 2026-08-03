package relay

import (
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/pkg/kafka"
	"github.com/soasurs/cordis/pkg/outbox"
)

func TestNewRejectsMissingDependencies(t *testing.T) {
	_, err := New(Config{})
	require.Error(t, err)
}

func TestNewRequiresListenerDSNForNotifyChannel(t *testing.T) {
	_, err := New(Config{
		DB:            new(sqlx.DB),
		Tables:        outbox.Tables{Streams: "streams", Events: "events"},
		Publisher:     &kafka.Publisher{},
		Namespace:     "cordis.message.outbox",
		NotifyChannel: "cordis_message_outbox",
	})
	require.Error(t, err)
}

func TestNewFillsDefaults(t *testing.T) {
	relay, err := New(Config{
		DB:        new(sqlx.DB),
		Tables:    outbox.Tables{Streams: "streams", Events: "events"},
		Publisher: &kafka.Publisher{},
		Namespace: "cordis.message.outbox",
	})
	require.NoError(t, err)
	require.Equal(t, 1, relay.cfg.Workers)
	require.Equal(t, 100, relay.cfg.BatchSize)
	require.Equal(t, time.Second, relay.cfg.PollInterval)
	require.Equal(t, 100*time.Millisecond, relay.cfg.TimeSlice)
	require.Equal(t, 100*time.Millisecond, relay.cfg.BackoffMin)
	require.Equal(t, time.Minute, relay.cfg.BackoffMax)
}

func TestBackoffIsBounded(t *testing.T) {
	relay, err := New(Config{
		DB:         new(sqlx.DB),
		Tables:     outbox.Tables{Streams: "streams", Events: "events"},
		Publisher:  &kafka.Publisher{},
		Namespace:  "cordis.message.outbox",
		BackoffMin: time.Second,
		BackoffMax: 10 * time.Second,
	})
	require.NoError(t, err)

	for attempt := 1; attempt <= 20; attempt++ {
		delay := relay.backoff(attempt)
		require.GreaterOrEqual(t, delay, time.Duration(float64(relay.cfg.BackoffMin)*0.9))
		require.LessOrEqual(t, delay, 10*time.Second)
	}
}

func TestHashNamespaceIsStable(t *testing.T) {
	require.Equal(t, hashNamespace("cordis.message.outbox"), hashNamespace("cordis.message.outbox"))
	require.NotEqual(t, hashNamespace("cordis.message.outbox"), hashNamespace("cordis.guild.outbox"))
}

func TestListenerDelayDoublesAndCaps(t *testing.T) {
	relay, err := New(Config{
		DB:         new(sqlx.DB),
		Tables:     outbox.Tables{Streams: "streams", Events: "events"},
		Publisher:  &kafka.Publisher{},
		Namespace:  "cordis.message.outbox",
		BackoffMin: time.Second,
		BackoffMax: 4 * time.Second,
	})
	require.NoError(t, err)

	require.Equal(t, 2*time.Second, relay.nextListenerDelay(time.Second))
	require.Equal(t, 4*time.Second, relay.nextListenerDelay(4*time.Second))
}
