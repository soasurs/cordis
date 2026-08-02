//go:build integration

package store

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

func testReadStates(t *testing.T, store Store) {
	const (
		userID    = int64(9501)
		channelID = int64(9601)
	)

	t.Run("ack list and no regress", func(t *testing.T) {
		ctx := t.Context()
		for _, messageID := range []int64{30, 50} {
			_, err := store.CreateMessage(ctx, CreateMessageParams{
				MessageID: messageID, ChannelID: channelID, AuthorID: userID,
				Content: "ack target", Type: 1,
			})
			require.NoError(t, err)
		}
		advanced, err := store.AckMessage(ctx, userID, channelID, 50)
		require.NoError(t, err)
		require.True(t, advanced)

		states, err := store.ListReadyChannelReadStates(ctx, userID, []int64{channelID})
		require.NoError(t, err)
		require.Len(t, states, 1)
		require.Equal(t, int64(50), states[0].LastMessageID)
		require.Equal(t, int64(50), states[0].LastReadMessageID)

		advanced, err = store.AckMessage(ctx, userID, channelID, 30)
		require.NoError(t, err)
		require.False(t, advanced)

		states, err = store.ListReadyChannelReadStates(ctx, userID, []int64{channelID})
		require.NoError(t, err)
		require.Len(t, states, 1)
		require.Equal(t, int64(50), states[0].LastReadMessageID)
	})

	t.Run("ack validates channel and permits deleted message", func(t *testing.T) {
		ctx := t.Context()
		_, err := store.AckMessage(ctx, userID, channelID, 9999)
		require.ErrorIs(t, err, sql.ErrNoRows)

		_, err = store.CreateMessage(ctx, CreateMessageParams{
			MessageID: 60, ChannelID: channelID + 1, AuthorID: userID,
			Content: "other channel", Type: 1,
		})
		require.NoError(t, err)
		_, err = store.AckMessage(ctx, userID, channelID, 60)
		require.ErrorIs(t, err, sql.ErrNoRows)

		_, err = store.CreateMessage(ctx, CreateMessageParams{
			MessageID: 70, ChannelID: channelID, AuthorID: userID,
			Content: "deleted ack target", Type: 1,
		})
		require.NoError(t, err)
		_, err = store.DeleteMessage(ctx, 70, userID, false)
		require.NoError(t, err)
		advanced, err := store.AckMessage(ctx, userID, channelID, 70)
		require.NoError(t, err)
		require.True(t, advanced)
	})

	t.Run("batch ready state", func(t *testing.T) {
		ctx := t.Context()
		const (
			batchUserID = int64(9511)
			channel1ID  = int64(9611)
			channel2ID  = int64(9612)
			channel3ID  = int64(9613)
		)

		_, err := store.CreateMessage(ctx, CreateMessageParams{
			MessageID: 9901, ChannelID: channel1ID, AuthorID: 9512,
			Content: "unread mention", Type: 1,
		})
		require.NoError(t, err)
		require.NoError(t, store.ReplaceMessageMentions(ctx, 9901, model.MessageMentions{UserIDs: []int64{batchUserID}}))

		_, err = store.CreateMessage(ctx, CreateMessageParams{
			MessageID: 9902, ChannelID: channel2ID, AuthorID: 9512,
			Content: "read", Type: 1,
		})
		require.NoError(t, err)
		advanced, err := store.AckMessage(ctx, batchUserID, channel2ID, 9902)
		require.NoError(t, err)
		require.True(t, advanced)

		_, err = store.CreateMessage(ctx, CreateMessageParams{
			MessageID: 9903, ChannelID: channel2ID, AuthorID: 9512,
			Content: "new unread mention", Type: 1,
		})
		require.NoError(t, err)
		require.NoError(t, store.ReplaceMessageMentions(ctx, 9903, model.MessageMentions{UserIDs: []int64{batchUserID}}))

		states, err := store.ListReadyChannelReadStates(
			ctx, batchUserID, []int64{channel2ID, channel1ID, channel3ID},
		)
		require.NoError(t, err)
		require.Len(t, states, 3)

		require.Equal(t, channel2ID, states[0].ChannelID)
		require.Equal(t, int64(9903), states[0].LastMessageID)
		require.Equal(t, int64(9902), states[0].LastReadMessageID)
		require.Equal(t, int32(1), states[0].MentionCount)

		require.Equal(t, channel1ID, states[1].ChannelID)
		require.Equal(t, int64(9901), states[1].LastMessageID)
		require.Zero(t, states[1].LastReadMessageID)
		require.Equal(t, int32(1), states[1].MentionCount)

		require.Equal(t, channel3ID, states[2].ChannelID)
		require.Zero(t, states[2].LastMessageID)
		require.Zero(t, states[2].LastReadMessageID)
		require.Zero(t, states[2].MentionCount)
	})

	t.Run("last message excludes deleted rows", func(t *testing.T) {
		ctx := t.Context()
		const channelID = int64(9621)
		for _, messageID := range []int64{9911, 9912} {
			_, err := store.CreateMessage(ctx, CreateMessageParams{
				MessageID: messageID, ChannelID: channelID, AuthorID: userID, Content: "head", Type: 1,
			})
			require.NoError(t, err)
		}
		lastMessageID, err := store.GetLastMessageID(ctx, channelID)
		require.NoError(t, err)
		require.Equal(t, int64(9912), lastMessageID)
		_, err = store.DeleteMessage(ctx, 9912, userID, false)
		require.NoError(t, err)
		lastMessageID, err = store.GetLastMessageID(ctx, channelID)
		require.NoError(t, err)
		require.Equal(t, int64(9911), lastMessageID)
	})
}
