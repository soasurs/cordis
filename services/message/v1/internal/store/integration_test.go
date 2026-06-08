//go:build integration

package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/internal/testpostgres"
	"github.com/soasurs/cordis/services/message/v1/db/migrations"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

func TestSQLStoreMessageLifecycle(t *testing.T) {
	ctx := context.Background()
	store := New(testpostgres.New(t, migrations.Files))

	root, err := store.CreateMessage(ctx, CreateMessageParams{
		MessageID:   100,
		ChannelID:   10,
		AuthorID:    20,
		Content:     "root",
		Type:        int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT),
		Attachments: []model.Attachment{{Key: "attachments/root.png", Filename: "root.png", Size: 10}},
	})
	if err != nil {
		t.Fatalf("create root message: %v", err)
	}
	if root.ID != 100 || len(root.Attachments) != 1 {
		t.Fatalf("unexpected root message: %+v", root)
	}

	if _, err := store.CreateMessage(ctx, CreateMessageParams{
		MessageID:           101,
		ChannelID:           10,
		AuthorID:            21,
		Content:             "reply",
		Type:                int32(messagev1.MessageType_MESSAGE_TYPE_REPLY),
		ReferencedMessageID: 100,
		ReferencedChannelID: 10,
	}); err != nil {
		t.Fatalf("create reply: %v", err)
	}
	if _, err := store.CreateMessage(ctx, CreateMessageParams{
		MessageID: 102,
		ChannelID: 10,
		AuthorID:  20,
		Content:   "newest",
		Type:      int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT),
	}); err != nil {
		t.Fatalf("create newest: %v", err)
	}

	before, err := store.ListMessages(ctx, ListMessagesParams{ChannelID: 10, Before: 102, Limit: 2})
	if err != nil {
		t.Fatalf("list before: %v", err)
	}
	if gotIDs(before); len(before) != 2 || before[0].ID != 101 || before[1].ID != 100 {
		t.Fatalf("unexpected before page: %v", gotIDs(before))
	}

	after, err := store.ListMessages(ctx, ListMessagesParams{ChannelID: 10, After: 100, Limit: 2})
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if len(after) != 2 || after[0].ID != 102 || after[1].ID != 101 {
		t.Fatalf("unexpected after page: %v", gotIDs(after))
	}

	around, err := store.ListMessages(ctx, ListMessagesParams{ChannelID: 10, Around: 101, Limit: 3})
	if err != nil {
		t.Fatalf("list around: %v", err)
	}
	if len(around) != 3 || around[0].ID != 102 || around[1].ID != 101 || around[2].ID != 100 {
		t.Fatalf("unexpected around page: %v", gotIDs(around))
	}

	updatedContent := "edited"
	updatedAttachments := []model.Attachment{{Key: "attachments/edited.png", Filename: "edited.png", Size: 20}}
	updated, err := store.UpdateMessage(ctx, UpdateMessageParams{
		MessageID:   100,
		ActorUserID: 20,
		Content:     &updatedContent,
		Attachments: &updatedAttachments,
	})
	if err != nil {
		t.Fatalf("update message: %v", err)
	}
	if updated.Content != "edited" || len(updated.Attachments) != 1 || updated.Attachments[0].Key != "attachments/edited.png" || updated.EditedAt == 0 {
		t.Fatalf("unexpected updated message: %+v", updated)
	}

	if err := store.ReplaceMessageMentions(ctx, 100, []int64{30, 30, 31}); err != nil {
		t.Fatalf("replace mentions: %v", err)
	}
	mentions, err := store.ListMentionUserIDs(ctx, 100)
	if err != nil {
		t.Fatalf("list mentions: %v", err)
	}
	if len(mentions) != 2 || mentions[0] != 30 || mentions[1] != 31 {
		t.Fatalf("unexpected mentions: %v", mentions)
	}
}

func TestSQLStoreReactionLifecycle(t *testing.T) {
	ctx := context.Background()
	store := New(testpostgres.New(t, migrations.Files))

	if _, err := store.CreateMessage(ctx, CreateMessageParams{
		MessageID: 100,
		ChannelID: 10,
		AuthorID:  20,
		Content:   "message",
		Type:      int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT),
	}); err != nil {
		t.Fatalf("create message: %v", err)
	}

	if err := store.AddReaction(ctx, 100, 30, 0, "🔥"); err != nil {
		t.Fatalf("add reaction: %v", err)
	}
	if err := store.AddReaction(ctx, 100, 30, 0, "🔥"); err != nil {
		t.Fatalf("add duplicate reaction: %v", err)
	}
	if err := store.AddReaction(ctx, 100, 31, 0, "🔥"); err != nil {
		t.Fatalf("add second reaction: %v", err)
	}

	summaries, err := store.ListReactionSummaries(ctx, []int64{100}, 30)
	if err != nil {
		t.Fatalf("list summaries: %v", err)
	}
	if len(summaries[100]) != 1 || summaries[100][0].Count != 2 || !summaries[100][0].Me {
		t.Fatalf("unexpected summaries: %+v", summaries[100])
	}

	users, err := store.ListReactionUsers(ctx, ReactionKey{MessageID: 100, EmojiID: 0, EmojiName: "🔥"}, 0, 10)
	if err != nil {
		t.Fatalf("list reaction users: %v", err)
	}
	if len(users) != 2 || users[0] != 30 || users[1] != 31 {
		t.Fatalf("unexpected users: %v", users)
	}

	if err := store.RemoveReaction(ctx, 100, 30, 0, "🔥"); err != nil {
		t.Fatalf("remove reaction: %v", err)
	}
	users, err = store.ListReactionUsers(ctx, ReactionKey{MessageID: 100, EmojiID: 0, EmojiName: "🔥"}, 0, 10)
	if err != nil {
		t.Fatalf("list reaction users after remove: %v", err)
	}
	if len(users) != 1 || users[0] != 31 {
		t.Fatalf("unexpected users after remove: %v", users)
	}
}

func TestSQLStoreDeletePermission(t *testing.T) {
	ctx := context.Background()
	store := New(testpostgres.New(t, migrations.Files))

	if _, err := store.CreateMessage(ctx, CreateMessageParams{
		MessageID: 100,
		ChannelID: 10,
		AuthorID:  20,
		Content:   "message",
		Type:      int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT),
	}); err != nil {
		t.Fatalf("create message: %v", err)
	}
	if err := store.DeleteMessage(ctx, 100, 21, false); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("DeleteMessage wrong actor = %v, want ErrPermissionDenied", err)
	}
	if err := store.DeleteMessage(ctx, 999, 21, false); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("DeleteMessage missing = %v, want sql.ErrNoRows", err)
	}
	if err := store.DeleteMessage(ctx, 100, 20, false); err != nil {
		t.Fatalf("delete message: %v", err)
	}
	if _, err := store.GetMessage(ctx, 100); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetMessage deleted = %v, want sql.ErrNoRows", err)
	}
}

func gotIDs(messages []*model.Message) []int64 {
	ids := make([]int64, 0, len(messages))
	for _, message := range messages {
		ids = append(ids, message.ID)
	}
	return ids
}
