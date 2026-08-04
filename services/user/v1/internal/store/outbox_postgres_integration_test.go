//go:build integration

package store

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/pkg/outbox"
	"github.com/soasurs/cordis/services/user/v1/internal/eventoutbox"
)

func testOutbox(t *testing.T, store Store, db *pgxpool.Pool) {
	t.Helper()
	streamKey := "user-42"
	require.NoError(t, store.EnsureUserStream(t.Context(), streamKey, 3))
	rangeValue, err := store.ReserveUserSequences(t.Context(), streamKey, 2)
	require.NoError(t, err)
	require.Equal(t, int64(1), rangeValue.FirstSequence)
	require.Equal(t, int64(2), rangeValue.LastSequence)
	require.Equal(t, 3, rangeValue.ShardID)

	records := []outbox.Record{
		{
			OutboxID: 7001, EventID: 9001, DeliveryIndex: 0,
			StreamKey: streamKey, RelayShardID: rangeValue.ShardID,
			StreamSequence: 1, Topic: "cordis.user.events.v1",
			EventType: "user.profile.updated", Key: []byte("42"),
			Payload: []byte(`{"t":"user.profile.updated"}`), CreatedAt: 1,
		},
		{
			OutboxID: 7002, EventID: 9001, DeliveryIndex: 1,
			StreamKey: streamKey, RelayShardID: rangeValue.ShardID,
			StreamSequence: 2, Topic: "cordis.user.events.v1",
			EventType: "relationship.updated", Key: []byte("42"),
			Payload: []byte(`{"t":"relationship.updated"}`), CreatedAt: 2,
		},
	}
	require.NoError(t, store.InsertUserOutbox(t.Context(), records))

	heads, err := outbox.SelectHeads(t.Context(), db, eventoutbox.UserEvents, rangeValue.ShardID, 2, 10)
	require.NoError(t, err)
	require.Len(t, heads, 1)
	require.Equal(t, int64(7001), heads[0].OutboxID)
	require.Equal(t, int64(1), heads[0].StreamSequence)

	require.NoError(t, outbox.DeleteDelivered(t.Context(), db, eventoutbox.UserEvents, []int64{7001}))
	require.NoError(t, outbox.UpdateFailed(t.Context(), db, eventoutbox.UserEvents, []outbox.FailedUpdate{
		{OutboxID: 7002, Attempts: 1, NextAttemptAt: 100},
	}))

	heads, err = outbox.SelectHeads(t.Context(), db, eventoutbox.UserEvents, rangeValue.ShardID, 50, 10)
	require.NoError(t, err)
	require.Len(t, heads, 0)
	heads, err = outbox.SelectHeads(t.Context(), db, eventoutbox.UserEvents, rangeValue.ShardID, 100, 10)
	require.NoError(t, err)
	require.Len(t, heads, 1)
	require.Equal(t, int64(7002), heads[0].OutboxID)
}
