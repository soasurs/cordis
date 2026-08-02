//go:build integration

package store

import (
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

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
			AssetID: 6001, Filename: "a.png", Size: 42, ContentType: "image/png", Width: 10, Height: 20,
		}},
	})
	require.NoError(t, err)
	require.Len(t, withAttachments.Attachments, 1)

	loaded, err := store.GetMessage(ctx, 5003)
	require.NoError(t, err)
	require.Equal(t, []model.Attachment{{
		AssetID: 6001, Filename: "a.png", Size: 42, ContentType: "image/png", Width: 10, Height: 20,
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
		Attachments: ptr([]model.Attachment{{AssetID: 6002, Filename: "b.png", Size: 1, ContentType: "image/png"}}),
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

	require.NoError(t, store.ReplaceMessageMentions(ctx, 5401, model.MessageMentions{UserIDs: []int64{4002, 4001, 4002, 0, -1}}))
	full, err := store.ListMessageMentions(ctx, 5401)
	require.NoError(t, err)
	require.Equal(t, []int64{4001, 4002}, full.UserIDs)

	require.NoError(t, store.ReplaceMessageMentions(ctx, 5401, model.MessageMentions{UserIDs: []int64{4003}}))
	full, err = store.ListMessageMentions(ctx, 5401)
	require.NoError(t, err)
	require.Equal(t, []int64{4003}, full.UserIDs)

	require.NoError(t, store.ReplaceMessageMentions(ctx, 5401, model.MessageMentions{}))
	full, err = store.ListMessageMentions(ctx, 5401)
	require.NoError(t, err)
	require.Empty(t, full.UserIDs)

	// Roles and @everyone are definitions stored beside direct user mentions.
	require.NoError(t, store.ReplaceMessageMentions(ctx, 5401, model.MessageMentions{
		UserIDs: []int64{4001}, RoleIDs: []int64{5002, 5001, 5002}, Everyone: true,
	}))
	full, err = store.ListMessageMentions(ctx, 5401)
	require.NoError(t, err)
	require.Equal(t, []int64{4001}, full.UserIDs)
	require.Equal(t, []int64{5001, 5002}, full.RoleIDs)
	require.True(t, full.Everyone)

	// Expanded rows for roles/everyone are replaced atomically under a
	// revision guard, leaving direct user mentions untouched.
	applied, err := store.RebuildExpandedMessageMentions(ctx, 5401, 1, []int64{6001, 6002, 6001})
	require.NoError(t, err)
	require.True(t, applied)
	full, err = store.ListMessageMentions(ctx, 5401)
	require.NoError(t, err)
	require.Equal(t, []int64{4001, 6001, 6002}, full.UserIDs)

	// A stale rebuild (message edited since the event) is skipped.
	applied, err = store.RebuildExpandedMessageMentions(ctx, 5401, 2, []int64{7001})
	require.NoError(t, err)
	require.False(t, applied)
	full, err = store.ListMessageMentions(ctx, 5401)
	require.NoError(t, err)
	require.Equal(t, []int64{4001, 6001, 6002}, full.UserIDs)

	// A current rebuild replaces the whole expanded set.
	applied, err = store.RebuildExpandedMessageMentions(ctx, 5401, 1, []int64{7001, 7002})
	require.NoError(t, err)
	require.True(t, applied)
	full, err = store.ListMessageMentions(ctx, 5401)
	require.NoError(t, err)
	require.Equal(t, []int64{4001, 7001, 7002}, full.UserIDs)
	require.Equal(t, []int64{5001, 5002}, full.RoleIDs)
	require.True(t, full.Everyone)

	// Deleted messages never accept a rebuild.
	_, err = store.CreateMessage(ctx, CreateMessageParams{
		MessageID: 5403, ChannelID: channelID, AuthorID: 3001,
		Content: "gone", Type: 1,
	})
	require.NoError(t, err)
	_, err = store.DeleteMessage(ctx, 5403, 3001, false)
	require.NoError(t, err)
	applied, err = store.RebuildExpandedMessageMentions(ctx, 5403, 2, []int64{8001})
	require.NoError(t, err)
	require.False(t, applied)

	// Batch loading returns one mentions struct per message.
	_, err = store.CreateMessage(ctx, CreateMessageParams{
		MessageID: 5402, ChannelID: channelID, AuthorID: 3001,
		Content: "m2", Type: 1,
	})
	require.NoError(t, err)
	require.NoError(t, store.ReplaceMessageMentions(ctx, 5402, model.MessageMentions{
		UserIDs: []int64{4009}, RoleIDs: []int64{5009}, Everyone: false,
	}))
	byMessage, err := store.ListMessagesMentions(ctx, []int64{5401, 5402, 9999})
	require.NoError(t, err)
	require.Equal(t, []int64{4001, 7001, 7002}, byMessage[5401].UserIDs)
	require.Equal(t, []int64{5001, 5002}, byMessage[5401].RoleIDs)
	require.True(t, byMessage[5401].Everyone)
	require.Equal(t, []int64{4009}, byMessage[5402].UserIDs)
	require.Equal(t, []int64{5009}, byMessage[5402].RoleIDs)
	require.False(t, byMessage[5402].Everyone)
	require.NotNil(t, byMessage[9999])
	require.Empty(t, byMessage[9999].UserIDs)
}

func testMessageIdempotency(t *testing.T, store Store) {
	ctx := t.Context()
	hash := []byte("12345678901234567890123456789012")

	claim, err := store.ClaimMessageIdempotency(ctx, ClaimMessageIdempotencyParams{
		ActorUserID:    9701,
		Operation:      "message.create",
		IdempotencyKey: "intent-1",
		RequestHash:    hash,
		MessageID:      5701,
		CreatedAt:      1000,
		ExpiresAt:      2000,
	})
	require.NoError(t, err)
	require.True(t, claim.Claimed)
	require.Equal(t, int64(5701), claim.MessageID)

	claim, err = store.ClaimMessageIdempotency(ctx, ClaimMessageIdempotencyParams{
		ActorUserID:    9701,
		Operation:      "message.create",
		IdempotencyKey: "intent-1",
		RequestHash:    []byte("abcdefghijklmnopqrstuvwxyz123456"),
		MessageID:      5702,
		CreatedAt:      1100,
		ExpiresAt:      2100,
	})
	require.NoError(t, err)
	require.False(t, claim.Claimed)
	require.Equal(t, int64(5701), claim.MessageID)
	require.Equal(t, hash, claim.RequestHash)

	err = store.Transact(ctx, func(tx Store) error {
		claim, err := tx.ClaimMessageIdempotency(ctx, ClaimMessageIdempotencyParams{
			ActorUserID:    9701,
			Operation:      "message.create",
			IdempotencyKey: "intent-rollback",
			RequestHash:    hash,
			MessageID:      5703,
			CreatedAt:      1000,
			ExpiresAt:      2000,
		})
		require.NoError(t, err)
		require.True(t, claim.Claimed)
		return errors.New("force idempotency rollback")
	})
	require.Error(t, err)

	claim, err = store.ClaimMessageIdempotency(ctx, ClaimMessageIdempotencyParams{
		ActorUserID:    9701,
		Operation:      "message.create",
		IdempotencyKey: "intent-rollback",
		RequestHash:    hash,
		MessageID:      5704,
		CreatedAt:      1000,
		ExpiresAt:      2000,
	})
	require.NoError(t, err)
	require.True(t, claim.Claimed)
	require.Equal(t, int64(5704), claim.MessageID)

	claim, err = store.ClaimMessageIdempotency(ctx, ClaimMessageIdempotencyParams{
		ActorUserID:    9701,
		Operation:      "message.create",
		IdempotencyKey: "intent-expired",
		RequestHash:    hash,
		MessageID:      5705,
		CreatedAt:      1000,
		ExpiresAt:      2000,
	})
	require.NoError(t, err)
	require.True(t, claim.Claimed)
	claim, err = store.ClaimMessageIdempotency(ctx, ClaimMessageIdempotencyParams{
		ActorUserID:    9701,
		Operation:      "message.create",
		IdempotencyKey: "intent-expired",
		RequestHash:    hash,
		MessageID:      5706,
		CreatedAt:      2000,
		ExpiresAt:      3000,
	})
	require.NoError(t, err)
	require.True(t, claim.Claimed)
	require.Equal(t, int64(5706), claim.MessageID)

	const concurrentRequests = 16
	results := make(chan *MessageIdempotencyClaim, concurrentRequests)
	errs := make(chan error, concurrentRequests)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range concurrentRequests {
		wg.Go(func() {
			<-start
			err := store.Transact(ctx, func(tx Store) error {
				claim, err := tx.ClaimMessageIdempotency(ctx, ClaimMessageIdempotencyParams{
					ActorUserID:    9701,
					Operation:      "message.create",
					IdempotencyKey: "intent-concurrent",
					RequestHash:    hash,
					MessageID:      int64(5800 + i),
					CreatedAt:      1000,
					ExpiresAt:      2000,
				})
				if err == nil {
					results <- claim
				}
				return err
			})
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)

	claims := make([]*MessageIdempotencyClaim, 0, concurrentRequests)
	for claim := range results {
		claims = append(claims, claim)
	}
	claimedCount := 0
	var winnerID int64
	for _, claim := range claims {
		if claim.Claimed {
			claimedCount++
			winnerID = claim.MessageID
		}
	}
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 1, claimedCount)
	require.NotZero(t, winnerID)
	for _, claim := range claims {
		require.Equal(t, winnerID, claim.MessageID)
	}
}
