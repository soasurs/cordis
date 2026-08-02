//go:build integration

package store

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

func testDmChannels(t *testing.T, store Store) {
	ctx := t.Context()

	t.Run("create", func(t *testing.T) {
		channel := &model.DmChannel{
			ID:        9101,
			UserLo:    9201,
			UserHi:    9202,
			CreatedAt: 1700000000000,
		}
		require.NoError(t, store.CreateDmChannel(ctx, channel))
	})

	t.Run("create pair conflict", func(t *testing.T) {
		channel := &model.DmChannel{
			ID:        9199,
			UserLo:    9201,
			UserHi:    9202,
			CreatedAt: 1700000000001,
		}
		require.ErrorIs(t, store.CreateDmChannel(ctx, channel), sql.ErrNoRows)
	})

	t.Run("get by id", func(t *testing.T) {
		loaded, err := store.GetDmChannel(ctx, 9101)
		require.NoError(t, err)
		require.Equal(t, int64(9101), loaded.ID)
		require.Equal(t, int64(9201), loaded.UserLo)
		require.Equal(t, int64(9202), loaded.UserHi)

		_, err = store.GetDmChannel(ctx, 9999)
		require.ErrorIs(t, err, sql.ErrNoRows)
	})

	t.Run("get by pair", func(t *testing.T) {
		loaded, err := store.GetDmChannelByPair(ctx, 9201, 9202)
		require.NoError(t, err)
		require.Equal(t, int64(9101), loaded.ID)

		_, err = store.GetDmChannelByPair(ctx, 9202, 9201)
		require.ErrorIs(t, err, sql.ErrNoRows)

		_, err = store.GetDmChannelByPair(ctx, 9399, 9400)
		require.ErrorIs(t, err, sql.ErrNoRows)
	})

	t.Run("list from lo perspective", func(t *testing.T) {
		require.NoError(t, store.CreateDmChannel(ctx, &model.DmChannel{
			ID: 9102, UserLo: 9201, UserHi: 9203, CreatedAt: 1700000000000,
		}))
		require.NoError(t, store.CreateDmChannel(ctx, &model.DmChannel{
			ID: 9103, UserLo: 9201, UserHi: 9204, CreatedAt: 1700000000000,
		}))

		channels, err := store.ListDmChannels(ctx, ListDmChannelsParams{UserID: 9201, Limit: 10})
		require.NoError(t, err)
		require.Len(t, channels, 3)
		require.Equal(t, []int64{9103, 9102, 9101}, dmChannelIDs(channels))
		channels, err = store.ListAllDmChannels(ctx, 9201)
		require.NoError(t, err)
		require.Equal(t, []int64{9103, 9102, 9101}, dmChannelIDs(channels))
	})

	t.Run("list from hi perspective", func(t *testing.T) {
		channels, err := store.ListDmChannels(ctx, ListDmChannelsParams{UserID: 9203, Limit: 10})
		require.NoError(t, err)
		require.Len(t, channels, 1)
		require.Equal(t, int64(9102), channels[0].ID)
	})

	t.Run("list with cursor and limit", func(t *testing.T) {
		channels, err := store.ListDmChannels(ctx, ListDmChannelsParams{UserID: 9201, BeforeID: 9102, Limit: 2})
		require.NoError(t, err)
		require.Len(t, channels, 1)
		require.Equal(t, int64(9101), channels[0].ID)

		channels, err = store.ListDmChannels(ctx, ListDmChannelsParams{UserID: 9201, Limit: 1})
		require.NoError(t, err)
		require.Len(t, channels, 1)
		require.Equal(t, int64(9103), channels[0].ID)

		empty, err := store.ListDmChannels(ctx, ListDmChannelsParams{UserID: 9399, Limit: 10})
		require.NoError(t, err)
		require.Empty(t, empty)
	})

	t.Run("check constraint user_hi > user_lo", func(t *testing.T) {
		err := store.CreateDmChannel(ctx, &model.DmChannel{
			ID: 9199, UserLo: 9299, UserHi: 9299, CreatedAt: 1700000000000,
		})
		requireCheckViolation(t, err)
	})
}

func dmChannelIDs(channels []*model.DmChannel) []int64 {
	out := make([]int64, 0, len(channels))
	for _, c := range channels {
		out = append(out, c.ID)
	}
	return out
}
