//go:build integration

package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

func testGuildChannelMutationLock(t *testing.T, store Store) {
	const guildID = int64(19300)
	ctx := t.Context()
	firstLocked := make(chan error)
	releaseFirst := make(chan struct{})
	firstResult := make(chan error)
	go func() {
		firstResult <- store.Transact(ctx, func(txStore Store) error {
			err := txStore.LockGuildChannelMutations(ctx, guildID)
			firstLocked <- err
			if err != nil {
				return err
			}
			<-releaseFirst
			return nil
		})
	}()
	require.NoError(t, <-firstLocked)

	secondAcquired := make(chan error)
	secondResult := make(chan error)
	go func() {
		secondResult <- store.Transact(ctx, func(txStore Store) error {
			err := txStore.LockGuildChannelMutations(ctx, guildID)
			secondAcquired <- err
			return err
		})
	}()
	var secondLockErr error
	acquiredEarly := false
	select {
	case err := <-secondAcquired:
		secondLockErr = err
		acquiredEarly = true
		require.Failf(t, "second lock acquired early", "error: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseFirst)
	require.NoError(t, <-firstResult)
	if !acquiredEarly {
		secondLockErr = <-secondAcquired
	}
	require.NoError(t, secondLockErr)
	require.NoError(t, <-secondResult)
}

func testGuildChannels(t *testing.T, store Store) {
	const guildID, ownerID = 10700, 20700
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, ownerID)

	cat, err := store.CreateGuildChannel(ctx, 10701, guildID, "Category", 3, 0, "", 0, now)
	require.NoError(t, err)
	require.Equal(t, int32(3), cat.Type)

	txt, err := store.CreateGuildChannel(ctx, 10702, guildID, "general", 1, 1, "welcome", cat.ID, now)
	require.NoError(t, err)
	require.Equal(t, "welcome", txt.Topic)
	require.Equal(t, cat.ID, txt.ParentID)

	_, err = store.CreateGuildChannel(ctx, 10703, guildID, "Voice", 2, 2, "", 0, now)
	require.NoError(t, err)

	snapshot, layoutRevision, err := store.ListGuildChannelsWithRevision(ctx, guildID)
	require.NoError(t, err)
	require.Len(t, snapshot, 3)
	require.Equal(t, int64(1), layoutRevision)
	batchSnapshot, batchRevisions, err := store.ListGuildChannelsWithRevisionsByGuilds(ctx, []int64{guildID})
	require.NoError(t, err)
	require.Len(t, batchSnapshot, 3)
	require.Equal(t, map[int64]int64{guildID: layoutRevision}, batchRevisions)

	loaded, err := store.GetGuildChannel(ctx, 10702)
	require.NoError(t, err)
	require.Equal(t, "general", loaded.Name)

	channels, err := store.ListGuildChannels(ctx, guildID)
	require.NoError(t, err)
	require.Equal(t, []int64{10701, 10702, 10703}, idsOf(channels, func(c *model.Channel) int64 { return c.ID }))
	channels, err = store.ListGuildChannelsByGuilds(ctx, []int64{guildID})
	require.NoError(t, err)
	require.Len(t, channels, 3)

	updated, err := store.UpdateGuildChannel(ctx, UpdateGuildChannelParams{
		ChannelID: 10702, Topic: ptr("desc"), ParentID: ptr(int64(0)), UpdatedAt: now,
	})
	require.NoError(t, err)
	require.Equal(t, "desc", updated.Topic)
	require.Equal(t, int64(0), updated.ParentID)
	require.Equal(t, int64(2), updated.Revision)

	_, err = store.UpdateGuildChannelPosition(ctx, guildID+1, 10702, 5, now)
	require.ErrorIs(t, err, sql.ErrNoRows)
	moved, err := store.UpdateGuildChannelPosition(ctx, guildID, 10702, 5, now)
	require.NoError(t, err)
	require.Equal(t, int32(5), moved.Position)
	updates := []GuildChannelPositionUpdate{
		{ChannelID: 10702, Position: 6, ParentID: cat.ID},
		{ChannelID: 10703, Position: 7, ParentID: cat.ID},
	}
	movedChannels, err := store.UpdateGuildChannelPositions(ctx, guildID+1, updates, now)
	require.NoError(t, err)
	require.Empty(t, movedChannels)
	movedChannels, err = store.UpdateGuildChannelPositions(ctx, guildID, updates, now)
	require.NoError(t, err)
	require.Len(t, movedChannels, 2)
	require.Equal(t, cat.ID, movedChannels[0].ParentID)
	require.Equal(t, cat.ID, movedChannels[1].ParentID)

	deleted, err := store.DeleteGuildChannel(ctx, cat.ID, now)
	require.NoError(t, err)
	require.True(t, deleted.DeletedAt > 0)
	_, err = store.GetGuildChannel(ctx, cat.ID)
	require.ErrorIs(t, err, sql.ErrNoRows)

	_, err = store.UpdateGuildChannel(ctx, UpdateGuildChannelParams{
		ChannelID: 10703, ParentID: ptr(cat.ID), UpdatedAt: now,
	})
	require.NoError(t, err)
	require.NoError(t, store.ClearGuildChannelParent(ctx, guildID, cat.ID, now))
	ch, err := store.GetGuildChannel(ctx, 10703)
	require.NoError(t, err)
	require.Equal(t, int64(0), ch.ParentID)

	require.NoError(t, store.DeleteGuildChannels(ctx, guildID, now))
	channels, err = store.ListGuildChannels(ctx, guildID)
	require.NoError(t, err)
	require.Empty(t, channels)
}

func testGuildChannelOverwrites(t *testing.T, store Store) {
	const guildID, ownerID, channelID, channel2ID = 10800, 20800, 10801, 10802
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, ownerID)
	_, err := store.CreateGuildChannel(ctx, channelID, guildID, "ch", 1, 0, "", 0, now)
	require.NoError(t, err)

	ow, err := store.UpsertGuildChannelPermissionOverwrite(ctx, &model.ChannelPermissionOverwrite{
		ChannelID: channelID, GuildID: guildID, AppliesTo: 1, AppliesToID: 20801,
		Allow: 1024, Deny: 0, CreatedAt: now,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1024), ow.Allow)
	require.Equal(t, int64(1), ow.Revision)

	_, err = store.UpsertGuildChannelPermissionOverwrite(ctx, &model.ChannelPermissionOverwrite{
		ChannelID: channelID, GuildID: guildID, AppliesTo: 2, AppliesToID: 20802,
		Allow: 0, Deny: 2048, CreatedAt: now,
	})
	require.NoError(t, err)

	ow2, err := store.UpsertGuildChannelPermissionOverwrite(ctx, &model.ChannelPermissionOverwrite{
		ChannelID: channelID, GuildID: guildID, AppliesTo: 1, AppliesToID: 20801,
		Allow: 4096, Deny: 0, CreatedAt: now,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(4096), ow2.Allow)
	require.Equal(t, int64(2), ow2.Revision)

	ows, err := store.ListGuildChannelPermissionOverwrites(ctx, channelID)
	require.NoError(t, err)
	require.Len(t, ows, 2)
	require.Equal(t, int32(1), ows[0].AppliesTo)
	require.Equal(t, int32(2), ows[1].AppliesTo)

	require.NoError(t, store.DeleteGuildChannelPermissionOverwrite(ctx, channelID, 1, 20801))
	ows, err = store.ListGuildChannelPermissionOverwrites(ctx, channelID)
	require.NoError(t, err)
	require.Len(t, ows, 1)

	require.NoError(t, store.DeleteGuildChannelPermissionOverwrites(ctx, channelID))
	ows, err = store.ListGuildChannelPermissionOverwrites(ctx, channelID)
	require.NoError(t, err)
	require.Empty(t, ows)

	_, err = store.CreateGuildChannel(ctx, channel2ID, guildID, "ch2", 1, 1, "", 0, now)
	require.NoError(t, err)
	for _, ch := range []int64{channelID, channel2ID} {
		_, err = store.UpsertGuildChannelPermissionOverwrite(ctx, &model.ChannelPermissionOverwrite{
			ChannelID: ch, GuildID: guildID, AppliesTo: 1, AppliesToID: 20899,
			Allow: 1, Deny: 0, CreatedAt: now,
		})
		require.NoError(t, err)
		_, err = store.UpsertGuildChannelPermissionOverwrite(ctx, &model.ChannelPermissionOverwrite{
			ChannelID: ch, GuildID: guildID, AppliesTo: 2, AppliesToID: 20898,
			Allow: 2, Deny: 0, CreatedAt: now,
		})
		require.NoError(t, err)
	}
	ows, err = store.ListGuildChannelPermissionOverwritesByGuild(ctx, guildID)
	require.NoError(t, err)
	require.Len(t, ows, 4)
	require.Equal(t, int64(channelID), ows[0].ChannelID)
	require.Equal(t, int64(channelID), ows[1].ChannelID)
	require.Equal(t, int64(channel2ID), ows[2].ChannelID)
	require.Equal(t, int64(channel2ID), ows[3].ChannelID)
	ows, err = store.ListGuildChannelPermissionOverwritesByChannels(ctx, []int64{channel2ID})
	require.NoError(t, err)
	require.Len(t, ows, 2)
	ows, err = store.ListGuildChannelPermissionOverwritesByGuilds(ctx, []int64{guildID}, 20898)
	require.NoError(t, err)
	require.Len(t, ows, 2)
	require.NoError(t, store.DeleteGuildChannelPermissionOverwritesForAppliesTo(ctx, guildID, 1, 20899))
	for _, ch := range []int64{channelID, channel2ID} {
		ows, err = store.ListGuildChannelPermissionOverwrites(ctx, ch)
		require.NoError(t, err)
		require.Len(t, ows, 1)
		require.Equal(t, int64(20898), ows[0].AppliesToID)
	}

	require.NoError(t, store.DeleteAllGuildChannelPermissionOverwrites(ctx, guildID))
	for _, ch := range []int64{channelID, channel2ID} {
		ows, err = store.ListGuildChannelPermissionOverwrites(ctx, ch)
		require.NoError(t, err)
		require.Empty(t, ows)
	}
}
