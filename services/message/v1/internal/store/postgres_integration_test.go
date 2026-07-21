//go:build integration

package store

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/migration"
	messagemigrations "github.com/soasurs/cordis/services/message/v1/db/migrations"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

// TestSQLStoreWithPostgres shares one PostgreSQL container across all
// integration subtests; each subtest works in its own channel/message ID
// space.
func TestSQLStoreWithPostgres(t *testing.T) {
	postgres := testkit.StartPostgres(t)
	db, err := database.NewPostgres(database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, migration.Apply(t.Context(), db, messagemigrations.Files))

	store := New(db)
	t.Run("create and get", func(t *testing.T) { testCreateAndGetMessage(t, store) })
	t.Run("list pagination", func(t *testing.T) { testListMessages(t, store) })
	t.Run("update", func(t *testing.T) { testUpdateMessage(t, store) })
	t.Run("delete", func(t *testing.T) { testDeleteMessage(t, store) })
	t.Run("mentions", func(t *testing.T) { testMessageMentions(t, store) })
	t.Run("transact rollback", func(t *testing.T) { testTransactRollback(t, store) })
	t.Run("constraint enforcement", func(t *testing.T) { testConstraintEnforcement(t, store) })
	t.Run("dm channels", func(t *testing.T) { testDmChannels(t, store) })
	t.Run("read states", func(t *testing.T) { testReadStates(t, store) })
}

func testCreateAndGetMessage(t *testing.T, store Store) {
	const channelID = int64(2001)
	ctx := t.Context()

	created, err := store.CreateMessage(ctx, CreateMessageParams{
		MessageID: 5001, ChannelID: channelID, AuthorID: 3001,
		Content: "hello", Type: 1,
	})
	require.NoError(t, err)
	require.Equal(t, int64(5001), created.ID)
	require.Equal(t, int64(1), created.Revision)
	require.True(t, created.CreatedAt > 0)

	reply, err := store.CreateMessage(ctx, CreateMessageParams{
		MessageID: 5002, ChannelID: channelID, AuthorID: 3002,
		Content: "reply", Type: 19,
		ReferencedMessageID: 5001, ReferencedChannelID: channelID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(5001), reply.ReferencedMessageID)
	require.Equal(t, channelID, reply.ReferencedChannelID)

	withAttachments, err := store.CreateMessage(ctx, CreateMessageParams{
		MessageID: 5003, ChannelID: channelID, AuthorID: 3001, Type: 1,
		Attachments: []model.Attachment{{
			Key: "k1", Filename: "a.png", Size: 42, ContentType: "image/png", Width: 10, Height: 20,
		}},
	})
	require.NoError(t, err)
	require.Len(t, withAttachments.Attachments, 1)

	loaded, err := store.GetMessage(ctx, 5003)
	require.NoError(t, err)
	require.Equal(t, []model.Attachment{{
		Key: "k1", Filename: "a.png", Size: 42, ContentType: "image/png", Width: 10, Height: 20,
	}}, loaded.Attachments)

	_, err = store.GetMessage(ctx, 9999)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func testListMessages(t *testing.T, store Store) {
	const channelID = int64(2002)
	ctx := t.Context()
	for i := int64(1); i <= 10; i++ {
		_, err := store.CreateMessage(ctx, CreateMessageParams{
			MessageID: 5100 + i, ChannelID: channelID, AuthorID: 3001,
			Content: "m", Type: 1,
		})
		require.NoError(t, err)
	}

	newest, err := store.ListMessages(ctx, ListMessagesParams{ChannelID: channelID, Limit: 3})
	require.NoError(t, err)
	require.Equal(t, []int64{5110, 5109, 5108}, messageIDs(newest))

	before, err := store.ListMessages(ctx, ListMessagesParams{ChannelID: channelID, Before: 5105, Limit: 3})
	require.NoError(t, err)
	require.Equal(t, []int64{5104, 5103, 5102}, messageIDs(before))

	after, err := store.ListMessages(ctx, ListMessagesParams{ChannelID: channelID, After: 5105, Limit: 3})
	require.NoError(t, err)
	require.Equal(t, []int64{5108, 5107, 5106}, messageIDs(after))

	around, err := store.ListMessages(ctx, ListMessagesParams{ChannelID: channelID, Around: 5105, Limit: 4})
	require.NoError(t, err)
	require.Equal(t, []int64{5107, 5106, 5105, 5104}, messageIDs(around))

	aroundEdge, err := store.ListMessages(ctx, ListMessagesParams{ChannelID: channelID, Around: 5110, Limit: 4})
	require.NoError(t, err)
	require.Equal(t, []int64{5110, 5109, 5108, 5107}, messageIDs(aroundEdge))

	_, err = store.DeleteMessage(ctx, 5109, 0, true)
	require.NoError(t, err)
	newest, err = store.ListMessages(ctx, ListMessagesParams{ChannelID: channelID, Limit: 3})
	require.NoError(t, err)
	require.Equal(t, []int64{5110, 5108, 5107}, messageIDs(newest))

	empty, err := store.ListMessages(ctx, ListMessagesParams{ChannelID: 9999, Limit: 3})
	require.NoError(t, err)
	require.Empty(t, empty)
}

func testUpdateMessage(t *testing.T, store Store) {
	const channelID, authorID = int64(2003), int64(3001)
	ctx := t.Context()
	_, err := store.CreateMessage(ctx, CreateMessageParams{
		MessageID: 5201, ChannelID: channelID, AuthorID: authorID,
		Content: "original", Type: 1,
	})
	require.NoError(t, err)

	updated, err := store.UpdateMessage(ctx, UpdateMessageParams{
		MessageID: 5201, ActorUserID: authorID, Content: ptr("edited"),
	})
	require.NoError(t, err)
	require.Equal(t, "edited", updated.Content)
	require.Equal(t, int64(2), updated.Revision)
	require.True(t, updated.EditedAt > 0)

	_, err = store.UpdateMessage(ctx, UpdateMessageParams{
		MessageID: 5201, ActorUserID: 9999, Content: ptr("hijack"),
	})
	require.ErrorIs(t, err, ErrPermissionDenied)

	modUpdated, err := store.UpdateMessage(ctx, UpdateMessageParams{
		MessageID: 5201, ActorUserID: 9999, HasModPermission: true,
		Flags: ptr(int32(4096)),
	})
	require.NoError(t, err)
	require.Equal(t, int32(4096), modUpdated.Flags)
	require.Equal(t, "edited", modUpdated.Content)
	require.Equal(t, int64(3), modUpdated.Revision)

	withAttachments, err := store.UpdateMessage(ctx, UpdateMessageParams{
		MessageID: 5201, ActorUserID: authorID,
		Attachments: ptr([]model.Attachment{{Key: "k2", Filename: "b.png", Size: 1, ContentType: "image/png"}}),
	})
	require.NoError(t, err)
	require.Len(t, withAttachments.Attachments, 1)

	_, err = store.UpdateMessage(ctx, UpdateMessageParams{
		MessageID: 9999, ActorUserID: authorID, Content: ptr("x"),
	})
	require.ErrorIs(t, err, sql.ErrNoRows)
	_, err = store.UpdateMessage(ctx, UpdateMessageParams{
		MessageID: 9999, ActorUserID: authorID, HasModPermission: true, Content: ptr("x"),
	})
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func testDeleteMessage(t *testing.T, store Store) {
	const channelID, authorID = int64(2004), int64(3001)
	ctx := t.Context()
	for _, messageID := range []int64{5301, 5302} {
		_, err := store.CreateMessage(ctx, CreateMessageParams{
			MessageID: messageID, ChannelID: channelID, AuthorID: authorID,
			Content: "m", Type: 1,
		})
		require.NoError(t, err)
	}

	_, err := store.DeleteMessage(ctx, 5301, 9999, false)
	require.ErrorIs(t, err, ErrPermissionDenied)

	deleted, err := store.DeleteMessage(ctx, 5301, authorID, false)
	require.NoError(t, err)
	require.True(t, deleted.DeletedAt > 0)
	require.Equal(t, int64(2), deleted.Revision)
	_, err = store.GetMessage(ctx, 5301)
	require.ErrorIs(t, err, sql.ErrNoRows)
	_, err = store.DeleteMessage(ctx, 5301, authorID, false)
	require.ErrorIs(t, err, sql.ErrNoRows)

	modDeleted, err := store.DeleteMessage(ctx, 5302, 9999, true)
	require.NoError(t, err)
	require.True(t, modDeleted.DeletedAt > 0)
	_, err = store.DeleteMessage(ctx, 5302, 9999, true)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func testMessageMentions(t *testing.T, store Store) {
	const channelID = int64(2005)
	ctx := t.Context()
	_, err := store.CreateMessage(ctx, CreateMessageParams{
		MessageID: 5401, ChannelID: channelID, AuthorID: 3001,
		Content: "m", Type: 1,
	})
	require.NoError(t, err)

	require.NoError(t, store.ReplaceMessageMentions(ctx, 5401, []int64{4002, 4001, 4002, 0, -1}))
	mentions, err := store.ListMentionUserIDs(ctx, 5401)
	require.NoError(t, err)
	require.Equal(t, []int64{4001, 4002}, mentions)

	require.NoError(t, store.ReplaceMessageMentions(ctx, 5401, []int64{4003}))
	mentions, err = store.ListMentionUserIDs(ctx, 5401)
	require.NoError(t, err)
	require.Equal(t, []int64{4003}, mentions)

	require.NoError(t, store.ReplaceMessageMentions(ctx, 5401, nil))
	mentions, err = store.ListMentionUserIDs(ctx, 5401)
	require.NoError(t, err)
	require.Empty(t, mentions)
}

func testTransactRollback(t *testing.T, store Store) {
	const channelID = int64(2006)
	ctx := t.Context()

	require.NoError(t, store.Transact(ctx, func(tx Store) error {
		if _, err := tx.CreateMessage(ctx, CreateMessageParams{
			MessageID: 5501, ChannelID: channelID, AuthorID: 3001,
			Content: "committed", Type: 1,
		}); err != nil {
			return err
		}
		return tx.ReplaceMessageMentions(ctx, 5501, []int64{4001})
	}))
	loaded, err := store.GetMessage(ctx, 5501)
	require.NoError(t, err)
	require.Equal(t, "committed", loaded.Content)

	err = store.Transact(ctx, func(tx Store) error {
		if _, err := tx.CreateMessage(ctx, CreateMessageParams{
			MessageID: 5502, ChannelID: channelID, AuthorID: 3001,
			Content: "rollback", Type: 1,
		}); err != nil {
			return err
		}
		return errors.New("force rollback")
	})
	require.Error(t, err)
	_, err = store.GetMessage(ctx, 5502)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func testConstraintEnforcement(t *testing.T, store Store) {
	const channelID = int64(2007)
	ctx := t.Context()

	_, err := store.CreateMessage(ctx, CreateMessageParams{
		MessageID: 5601, ChannelID: channelID, AuthorID: 3001,
		Content: "", Type: 1,
	})
	requireCheckViolation(t, err)

	_, err = store.CreateMessage(ctx, CreateMessageParams{
		MessageID: 5602, ChannelID: channelID, AuthorID: 3001,
		Content: "m", Type: 2,
	})
	requireCheckViolation(t, err)

	_, err = store.CreateMessage(ctx, CreateMessageParams{
		MessageID: 5603, ChannelID: channelID, AuthorID: 3001,
		Content: "reply without reference", Type: 19,
	})
	requireCheckViolation(t, err)
}

func messageIDs(messages []*model.Message) []int64 {
	out := make([]int64, 0, len(messages))
	for _, message := range messages {
		out = append(out, message.ID)
	}
	return out
}

func ptr[T any](v T) *T { return &v }

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

func requireCheckViolation(t *testing.T, err error) {
	t.Helper()
	var pqErr *pq.Error
	require.True(t, errors.As(err, &pqErr), "expected pq.Error, got %v", err)
	require.Equal(t, pq.ErrorCode("23514"), pqErr.Code)
}

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
		require.NoError(t, store.ReplaceMessageMentions(ctx, 9901, []int64{batchUserID}))

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
		require.NoError(t, store.ReplaceMessageMentions(ctx, 9903, []int64{batchUserID}))

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
