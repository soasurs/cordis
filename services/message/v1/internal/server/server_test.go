package server

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"testing"

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/pkg/outbox"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
	"github.com/soasurs/cordis/services/message/v1/internal/svc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCreateMessage(t *testing.T) {
	fake := newFakeStore()
	server := newTestMessageServer(t, fake)

	req := new(messagev1.CreateMessageRequest)
	req.SetChannelId(10)
	req.SetAuthorId(20)
	req.SetContent("hello")
	req.SetType(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)
	req.SetFlags(int32(messagev1.MessageFlag_MESSAGE_FLAG_SUPPRESS_NOTIFICATIONS))
	req.SetAttachments([]*messagev1.Attachment{pbAttachment("attachments/1/a.png")})
	req.SetMentionUserIds([]int64{30, 31})

	resp, err := server.CreateMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateMessage returned error: %v", err)
	}
	if resp.GetMessage().GetId() == 0 ||
		resp.GetMessage().GetContent() != "hello" ||
		resp.GetMessage().GetFlags() != int32(messagev1.MessageFlag_MESSAGE_FLAG_SUPPRESS_NOTIFICATIONS) {
		t.Fatalf("unexpected response: %v", resp)
	}
	if got := fake.mentions[resp.GetMessage().GetId()]; len(got) != 2 || got[0] != 30 || got[1] != 31 {
		t.Fatalf("unexpected mentions: %v", got)
	}
}

func TestCreateReplyValidatesReferencedChannel(t *testing.T) {
	fake := newFakeStore()
	fake.messages[100] = &model.Message{ID: 100, ChannelID: 10, AuthorID: 20, Content: "root", Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)}
	server := newTestMessageServer(t, fake)

	req := new(messagev1.CreateMessageRequest)
	req.SetChannelId(10)
	req.SetAuthorId(20)
	req.SetContent("reply")
	req.SetType(messagev1.MessageType_MESSAGE_TYPE_REPLY)
	req.SetReferencedMessageId(100)
	req.SetReferencedChannelId(11)

	_, err := server.CreateMessage(context.Background(), req)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("CreateMessage code = %v, want %v: %v", status.Code(err), codes.InvalidArgument, err)
	}
}

func TestUpdateMessageReplacesAttachmentsAndMentions(t *testing.T) {
	fake := newFakeStore()
	fake.messages[100] = &model.Message{
		ID:          100,
		ChannelID:   10,
		AuthorID:    20,
		Content:     "old",
		Type:        int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT),
		Attachments: []model.Attachment{{Key: "attachments/old.png", Filename: "old.png"}},
	}
	server := newTestMessageServer(t, fake)

	req := new(messagev1.UpdateMessageRequest)
	req.SetMessageId(100)
	req.SetActorUserId(20)
	req.SetContent("edited")
	attachmentList := new(messagev1.AttachmentList)
	attachmentList.SetAttachments([]*messagev1.Attachment{pbAttachment("attachments/new.png")})
	req.SetAttachments(attachmentList)
	mentionList := new(messagev1.MentionList)
	mentionList.SetUserIds([]int64{30})
	req.SetMentions(mentionList)

	resp, err := server.UpdateMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("UpdateMessage returned error: %v", err)
	}
	if resp.GetMessage().GetContent() != "edited" || len(resp.GetMessage().GetAttachments()) != 1 {
		t.Fatalf("unexpected response: %v", resp)
	}
	if got := fake.mentions[100]; len(got) != 1 || got[0] != 30 {
		t.Fatalf("unexpected mentions: %v", got)
	}
}

func TestUpdateMessagePermissionDenied(t *testing.T) {
	fake := newFakeStore()
	fake.messages[100] = &model.Message{ID: 100, ChannelID: 10, AuthorID: 20, Content: "old", Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)}
	server := newTestMessageServer(t, fake)

	req := new(messagev1.UpdateMessageRequest)
	req.SetMessageId(100)
	req.SetActorUserId(21)
	req.SetContent("edited")

	_, err := server.UpdateMessage(context.Background(), req)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("UpdateMessage code = %v, want %v: %v", status.Code(err), codes.PermissionDenied, err)
	}
	if !rpcerror.Is(err, rpcerror.MessageDomain, rpcerror.MessagePermissionDenied) {
		t.Fatalf("expected message permission error info: %v", err)
	}
}

func TestListMessagesWithReactionSummaries(t *testing.T) {
	fake := newFakeStore()
	fake.messages[100] = &model.Message{ID: 100, ChannelID: 10, AuthorID: 20, Content: "one", Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)}
	fake.messages[101] = &model.Message{ID: 101, ChannelID: 10, AuthorID: 20, Content: "two", Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)}
	fake.reactions[reactionKey{messageID: 101, userID: 30, emojiID: 0, emojiName: "🔥"}] = struct{}{}
	server := newTestMessageServer(t, fake)

	req := new(messagev1.ListMessagesRequest)
	req.SetChannelId(10)
	req.SetBefore(200)
	req.SetViewerUserId(30)

	resp, err := server.ListMessages(context.Background(), req)
	if err != nil {
		t.Fatalf("ListMessages returned error: %v", err)
	}
	if len(resp.GetMessages()) != 2 || resp.GetMessages()[0].GetId() != 101 || resp.GetMessages()[1].GetId() != 100 {
		t.Fatalf("unexpected messages: %v", resp.GetMessages())
	}
	if resp.GetBeforeCursor() != 100 || resp.GetAfterCursor() != 101 {
		t.Fatalf("unexpected cursors before=%d after=%d", resp.GetBeforeCursor(), resp.GetAfterCursor())
	}
	reactions := resp.GetReactions()[101].GetReactions()
	if len(reactions) != 1 || reactions[0].GetCount() != 1 || !reactions[0].GetMe() || reactions[0].GetEmoji().GetId() != 0 || reactions[0].GetEmoji().GetName() != "🔥" {
		t.Fatalf("unexpected reactions: %v", reactions)
	}
}

func TestReactionLifecycle(t *testing.T) {
	fake := newFakeStore()
	fake.messages[100] = &model.Message{ID: 100, ChannelID: 10, AuthorID: 20, Content: "one", Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)}
	server := newTestMessageServer(t, fake)

	addReq := new(messagev1.AddReactionRequest)
	addReq.SetMessageId(100)
	addReq.SetUserId(30)
	addReq.SetEmojiId(0)
	addReq.SetEmojiName("🔥")
	if _, err := server.AddReaction(context.Background(), addReq); err != nil {
		t.Fatalf("AddReaction returned error: %v", err)
	}

	listReq := new(messagev1.ListReactionUsersRequest)
	listReq.SetMessageId(100)
	listReq.SetEmojiId(0)
	listReq.SetEmojiName("🔥")
	resp, err := server.ListReactionUsers(context.Background(), listReq)
	if err != nil {
		t.Fatalf("ListReactionUsers returned error: %v", err)
	}
	if len(resp.GetUserIds()) != 1 || resp.GetUserIds()[0] != 30 {
		t.Fatalf("unexpected users: %v", resp.GetUserIds())
	}

	removeReq := new(messagev1.RemoveReactionRequest)
	removeReq.SetMessageId(100)
	removeReq.SetUserId(30)
	removeReq.SetEmojiId(0)
	removeReq.SetEmojiName("🔥")
	if _, err := server.RemoveReaction(context.Background(), removeReq); err != nil {
		t.Fatalf("RemoveReaction returned error: %v", err)
	}
	if len(fake.reactions) != 0 {
		t.Fatalf("expected reaction to be removed: %v", fake.reactions)
	}
}

func newTestMessageServer(t *testing.T, fakeStore store.Store) messagev1.MessageServiceServer {
	t.Helper()

	node, err := snowflake.New()
	if err != nil {
		t.Fatalf("new snowflake node: %v", err)
	}

	return New(&svc.ServiceContext{
		Store:     fakeStore,
		Snowflake: node,
	})
}

func pbAttachment(key string) *messagev1.Attachment {
	attachment := new(messagev1.Attachment)
	attachment.SetKey(key)
	attachment.SetFilename("file.png")
	attachment.SetSize(10)
	attachment.SetContentType("image/png")
	attachment.SetWidth(1)
	attachment.SetHeight(1)
	return attachment
}

type reactionKey struct {
	messageID int64
	userID    int64
	emojiID   int64
	emojiName string
}

type fakeStore struct {
	messages  map[int64]*model.Message
	mentions  map[int64][]int64
	reactions map[reactionKey]struct{}
	outbox    map[int64]*outbox.Event
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		messages:  make(map[int64]*model.Message),
		mentions:  make(map[int64][]int64),
		reactions: make(map[reactionKey]struct{}),
		outbox:    make(map[int64]*outbox.Event),
	}
}

func (s *fakeStore) Transact(_ context.Context, fn func(txStore store.Store) error) error {
	return fn(s)
}

func (s *fakeStore) CreateMessage(_ context.Context, params store.CreateMessageParams) (*model.Message, error) {
	if _, ok := s.messages[params.MessageID]; ok {
		return nil, errors.New("duplicate message")
	}
	message := &model.Message{
		ID:                  params.MessageID,
		ChannelID:           params.ChannelID,
		AuthorID:            params.AuthorID,
		Content:             params.Content,
		Type:                params.Type,
		Flags:               params.Flags,
		ReferencedMessageID: params.ReferencedMessageID,
		ReferencedChannelID: params.ReferencedChannelID,
		Attachments:         append([]model.Attachment(nil), params.Attachments...),
		CreatedAt:           1,
	}
	s.messages[message.ID] = message
	return cloneMessage(message), nil
}

func (s *fakeStore) GetMessage(_ context.Context, messageID int64) (*model.Message, error) {
	message, ok := s.messages[messageID]
	if !ok || message.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	return cloneMessage(message), nil
}

func (s *fakeStore) ListMessages(_ context.Context, params store.ListMessagesParams) ([]*model.Message, error) {
	var messages []*model.Message
	for _, message := range s.messages {
		if message.ChannelID != params.ChannelID || message.DeletedAt != 0 {
			continue
		}
		if params.Before != 0 && message.ID >= params.Before {
			continue
		}
		if params.After != 0 && message.ID <= params.After {
			continue
		}
		messages = append(messages, cloneMessage(message))
	}
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].ID > messages[j].ID
	})
	if params.Limit > 0 && len(messages) > params.Limit {
		messages = messages[:params.Limit]
	}
	return messages, nil
}

func (s *fakeStore) UpdateMessage(_ context.Context, params store.UpdateMessageParams) (*model.Message, error) {
	message, ok := s.messages[params.MessageID]
	if !ok || message.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	if !params.HasModPermission && message.AuthorID != params.ActorUserID {
		return nil, store.ErrPermissionDenied
	}
	if params.Content != nil {
		message.Content = *params.Content
	}
	if params.Flags != nil {
		message.Flags = *params.Flags
	}
	if params.Attachments != nil {
		message.Attachments = append([]model.Attachment(nil), (*params.Attachments)...)
	}
	message.EditedAt = 2
	message.UpdatedAt = 2
	return cloneMessage(message), nil
}

func (s *fakeStore) DeleteMessage(_ context.Context, messageID, actorUserID int64, hasModPermission bool) error {
	message, ok := s.messages[messageID]
	if !ok || message.DeletedAt != 0 {
		return sql.ErrNoRows
	}
	if !hasModPermission && message.AuthorID != actorUserID {
		return store.ErrPermissionDenied
	}
	message.DeletedAt = 3
	message.UpdatedAt = 3
	return nil
}

func (s *fakeStore) ReplaceMessageMentions(_ context.Context, messageID int64, userIDs []int64) error {
	s.mentions[messageID] = append([]int64(nil), userIDs...)
	sort.Slice(s.mentions[messageID], func(i, j int) bool {
		return s.mentions[messageID][i] < s.mentions[messageID][j]
	})
	return nil
}

func (s *fakeStore) ListMentionUserIDs(_ context.Context, messageID int64) ([]int64, error) {
	return append([]int64(nil), s.mentions[messageID]...), nil
}

func (s *fakeStore) AddReaction(_ context.Context, messageID, userID, emojiID int64, emojiName string) error {
	s.reactions[reactionKey{messageID: messageID, userID: userID, emojiID: emojiID, emojiName: emojiName}] = struct{}{}
	return nil
}

func (s *fakeStore) RemoveReaction(_ context.Context, messageID, userID, emojiID int64, emojiName string) error {
	delete(s.reactions, reactionKey{messageID: messageID, userID: userID, emojiID: emojiID, emojiName: emojiName})
	return nil
}

func (s *fakeStore) ListReactionSummaries(_ context.Context, messageIDs []int64, viewerUserID int64) (map[int64][]*model.ReactionSummary, error) {
	messageSet := make(map[int64]struct{}, len(messageIDs))
	for _, messageID := range messageIDs {
		messageSet[messageID] = struct{}{}
	}
	byEmoji := make(map[store.ReactionKey]*model.ReactionSummary)
	for key := range s.reactions {
		if _, ok := messageSet[key.messageID]; !ok {
			continue
		}
		summaryKey := store.ReactionKey{MessageID: key.messageID, EmojiID: key.emojiID, EmojiName: key.emojiName}
		summary := byEmoji[summaryKey]
		if summary == nil {
			summary = &model.ReactionSummary{Emoji: model.Emoji{ID: key.emojiID, Name: key.emojiName}}
			byEmoji[summaryKey] = summary
		}
		summary.Count++
		if key.userID == viewerUserID {
			summary.Me = true
		}
	}
	values := make(map[int64][]*model.ReactionSummary)
	for key, summary := range byEmoji {
		values[key.MessageID] = append(values[key.MessageID], summary)
	}
	return values, nil
}

func (s *fakeStore) ListReactionUsers(_ context.Context, key store.ReactionKey, cursor int64, limit int) ([]int64, error) {
	var userIDs []int64
	for reaction := range s.reactions {
		if reaction.messageID != key.MessageID || reaction.emojiID != key.EmojiID || reaction.emojiName != key.EmojiName || reaction.userID <= cursor {
			continue
		}
		userIDs = append(userIDs, reaction.userID)
	}
	sort.Slice(userIDs, func(i, j int) bool {
		return userIDs[i] < userIDs[j]
	})
	if limit > 0 && len(userIDs) > limit {
		userIDs = userIDs[:limit]
	}
	return userIDs, nil
}

func (s *fakeStore) InsertOutboxEvent(_ context.Context, evt outbox.Event) error {
	s.outbox[evt.ID] = &evt
	return nil
}

func cloneMessage(message *model.Message) *model.Message {
	clone := *message
	clone.Attachments = append([]model.Attachment(nil), message.Attachments...)
	return &clone
}
