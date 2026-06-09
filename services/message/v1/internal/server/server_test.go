package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"testing"

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/pkg/outbox"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/message/v1/config"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
	"github.com/soasurs/cordis/services/message/v1/internal/svc"
	"github.com/stretchr/testify/require"
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

	resp, err := server.CreateMessage(t.Context(), req)
	require.NoError(t, err)
	require.NotZero(t, resp.GetMessage().GetId())
	require.Equal(t, "hello", resp.GetMessage().GetContent())
	require.Equal(t, int32(messagev1.MessageFlag_MESSAGE_FLAG_SUPPRESS_NOTIFICATIONS), resp.GetMessage().GetFlags())
	got := fake.mentions[resp.GetMessage().GetId()]
	require.Len(t, got, 2)
	require.Equal(t, int64(30), got[0])
	require.Equal(t, int64(31), got[1])
	evt := onlyOutboxEvent(t, fake)
	var envelope eventEnvelope[messageCreatedPayload]
	require.NoError(t, json.Unmarshal(evt.Payload, &envelope))
	require.Equal(t, evt.ID, envelope.EventID)
	require.Equal(t, EventTypeMessageCreated, envelope.EventType)
	require.Equal(t, eventSchemaVersion, envelope.SchemaVersion)
	require.Equal(t, resp.GetMessage().GetId(), envelope.Data.MessageID)
	require.Equal(t, int64(10), envelope.Data.ChannelID)
	require.Equal(t, "message.events", evt.Topic)
	require.Equal(t, "10", string(evt.Key))
	require.Equal(t, outbox.PartitionForKey(evt.Key, outbox.DefaultPartitionCount), evt.Partition)
	require.Equal(t, 7, evt.MaxRetries)
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

	_, err := server.CreateMessage(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
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

	resp, err := server.UpdateMessage(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, "edited", resp.GetMessage().GetContent())
	require.Len(t, resp.GetMessage().GetAttachments(), 1)
	mentionGot := fake.mentions[100]
	require.Len(t, mentionGot, 1)
	require.Equal(t, int64(30), mentionGot[0])
	evt := onlyOutboxEvent(t, fake)
	var envelope eventEnvelope[messageUpdatedPayload]
	require.NoError(t, json.Unmarshal(evt.Payload, &envelope))
	require.Equal(t, EventTypeMessageUpdated, envelope.EventType)
	require.Equal(t, int64(100), envelope.Data.MessageID)
}

func TestUpdateMessagePermissionDenied(t *testing.T) {
	fake := newFakeStore()
	fake.messages[100] = &model.Message{ID: 100, ChannelID: 10, AuthorID: 20, Content: "old", Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)}
	server := newTestMessageServer(t, fake)

	req := new(messagev1.UpdateMessageRequest)
	req.SetMessageId(100)
	req.SetActorUserId(21)
	req.SetContent("edited")

	_, err := server.UpdateMessage(t.Context(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.MessageDomain, rpcerror.MessagePermissionDenied))
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

	resp, err := server.ListMessages(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetMessages(), 2)
	require.Equal(t, int64(101), resp.GetMessages()[0].GetId())
	require.Equal(t, int64(100), resp.GetMessages()[1].GetId())
	require.Equal(t, int64(100), resp.GetBeforeCursor())
	require.Equal(t, int64(101), resp.GetAfterCursor())
	reactions := resp.GetReactions()[101].GetReactions()
	require.Len(t, reactions, 1)
	require.Equal(t, int64(1), reactions[0].GetCount())
	require.True(t, reactions[0].GetMe())
	require.Equal(t, int64(0), reactions[0].GetEmoji().GetId())
	require.Equal(t, "🔥", reactions[0].GetEmoji().GetName())
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
	_, err := server.AddReaction(t.Context(), addReq)
	require.NoError(t, err)

	listReq := new(messagev1.ListReactionUsersRequest)
	listReq.SetMessageId(100)
	listReq.SetEmojiId(0)
	listReq.SetEmojiName("🔥")
	resp, err := server.ListReactionUsers(t.Context(), listReq)
	require.NoError(t, err)
	require.Len(t, resp.GetUserIds(), 1)
	require.Equal(t, int64(30), resp.GetUserIds()[0])

	removeReq := new(messagev1.RemoveReactionRequest)
	removeReq.SetMessageId(100)
	removeReq.SetUserId(30)
	removeReq.SetEmojiId(0)
	removeReq.SetEmojiName("🔥")
	_, err = server.RemoveReaction(t.Context(), removeReq)
	require.NoError(t, err)
	require.Empty(t, fake.reactions)
}

func TestDeleteMessage(t *testing.T) {
	fake := newFakeStore()
	fake.messages[100] = &model.Message{ID: 100, ChannelID: 10, AuthorID: 20, Content: "hello", Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)}
	server := newTestMessageServer(t, fake)

	req := new(messagev1.DeleteMessageRequest)
	req.SetMessageId(100)
	req.SetActorUserId(20)

	resp, err := server.DeleteMessage(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.NotZero(t, fake.messages[100].DeletedAt)

	evt := onlyOutboxEvent(t, fake)
	var envelope eventEnvelope[messageDeletedPayload]
	require.NoError(t, json.Unmarshal(evt.Payload, &envelope))
	require.Equal(t, EventTypeMessageDeleted, envelope.EventType)
	require.Equal(t, int64(100), envelope.Data.MessageID)
	require.Equal(t, int64(10), envelope.Data.ChannelID)
}

func TestDeleteMessagePermissionDenied(t *testing.T) {
	fake := newFakeStore()
	fake.messages[100] = &model.Message{ID: 100, ChannelID: 10, AuthorID: 20, Content: "hello", Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)}
	server := newTestMessageServer(t, fake)

	req := new(messagev1.DeleteMessageRequest)
	req.SetMessageId(100)
	req.SetActorUserId(21)

	_, err := server.DeleteMessage(t.Context(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestDeleteMessageNotFound(t *testing.T) {
	fake := newFakeStore()
	server := newTestMessageServer(t, fake)

	req := new(messagev1.DeleteMessageRequest)
	req.SetMessageId(999)
	req.SetActorUserId(20)

	_, err := server.DeleteMessage(t.Context(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestGetMessage(t *testing.T) {
	fake := newFakeStore()
	fake.messages[100] = &model.Message{ID: 100, ChannelID: 10, AuthorID: 20, Content: "hello", Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)}
	fake.reactions[reactionKey{messageID: 100, userID: 30, emojiID: 0, emojiName: "🔥"}] = struct{}{}
	server := newTestMessageServer(t, fake)

	req := new(messagev1.GetMessageRequest)
	req.SetMessageId(100)
	req.SetViewerUserId(30)

	resp, err := server.GetMessage(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int64(100), resp.GetMessage().GetId())
	require.Equal(t, "hello", resp.GetMessage().GetContent())
	reactions := resp.GetReactions()
	require.Len(t, reactions, 1)
	require.Equal(t, "🔥", reactions[0].GetEmoji().GetName())
	require.Equal(t, int64(1), reactions[0].GetCount())
	require.True(t, reactions[0].GetMe())
}

func TestGetMessageNotFound(t *testing.T) {
	fake := newFakeStore()
	server := newTestMessageServer(t, fake)

	req := new(messagev1.GetMessageRequest)
	req.SetMessageId(999)

	_, err := server.GetMessage(t.Context(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestCreateMessageValidation(t *testing.T) {
	fake := newFakeStore()
	server := newTestMessageServer(t, fake)

	tests := []struct {
		name string
		req  *messagev1.CreateMessageRequest
	}{
		{
			name: "missing channel id",
			req:  func() *messagev1.CreateMessageRequest { r := new(messagev1.CreateMessageRequest); r.SetAuthorId(1); r.SetContent("hi"); return r }(),
		},
		{
			name: "missing author id",
			req:  func() *messagev1.CreateMessageRequest { r := new(messagev1.CreateMessageRequest); r.SetChannelId(1); r.SetContent("hi"); return r }(),
		},
		{
			name: "empty content no attachments",
			req:  func() *messagev1.CreateMessageRequest { r := new(messagev1.CreateMessageRequest); r.SetChannelId(1); r.SetAuthorId(1); return r }(),
		},
		{
			name: "content too long",
			req: func() *messagev1.CreateMessageRequest {
				r := new(messagev1.CreateMessageRequest)
				r.SetChannelId(1)
				r.SetAuthorId(1)
				r.SetContent(string(make([]byte, 2001)))
				return r
			}(),
		},
		{
			name: "invalid flags",
			req: func() *messagev1.CreateMessageRequest {
				r := new(messagev1.CreateMessageRequest)
				r.SetChannelId(1)
				r.SetAuthorId(1)
				r.SetContent("hi")
				r.SetFlags(-1)
				return r
			}(),
		},
		{
			name: "reply without referenced message",
			req: func() *messagev1.CreateMessageRequest {
				r := new(messagev1.CreateMessageRequest)
				r.SetChannelId(1)
				r.SetAuthorId(1)
				r.SetContent("hi")
				r.SetType(messagev1.MessageType_MESSAGE_TYPE_REPLY)
				return r
			}(),
		},
		{
			name: "non-reply with referenced message",
			req: func() *messagev1.CreateMessageRequest {
				r := new(messagev1.CreateMessageRequest)
				r.SetChannelId(1)
				r.SetAuthorId(1)
				r.SetContent("hi")
				r.SetReferencedMessageId(100)
				r.SetReferencedChannelId(1)
				return r
			}(),
		},
		{
			name: "invalid mention user id",
			req: func() *messagev1.CreateMessageRequest {
				r := new(messagev1.CreateMessageRequest)
				r.SetChannelId(1)
				r.SetAuthorId(1)
				r.SetContent("hi")
				r.SetMentionUserIds([]int64{-1})
				return r
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := server.CreateMessage(t.Context(), tt.req)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func TestUpdateMessageValidation(t *testing.T) {
	t.Run("missing message id", func(t *testing.T) {
		server := newTestMessageServer(t, newFakeStore())
		req := new(messagev1.UpdateMessageRequest)
		req.SetActorUserId(20)
		req.SetContent("new")
		_, err := server.UpdateMessage(t.Context(), req)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("missing actor user id", func(t *testing.T) {
		fake := newFakeStore()
		fake.messages[100] = &model.Message{ID: 100, ChannelID: 10, AuthorID: 20, Content: "old", Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)}
		server := newTestMessageServer(t, fake)
		req := new(messagev1.UpdateMessageRequest)
		req.SetMessageId(100)
		req.SetContent("new")
		_, err := server.UpdateMessage(t.Context(), req)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("no fields to update", func(t *testing.T) {
		fake := newFakeStore()
		fake.messages[100] = &model.Message{ID: 100, ChannelID: 10, AuthorID: 20, Content: "old", Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)}
		server := newTestMessageServer(t, fake)
		req := new(messagev1.UpdateMessageRequest)
		req.SetMessageId(100)
		req.SetActorUserId(20)
		_, err := server.UpdateMessage(t.Context(), req)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("message not found", func(t *testing.T) {
		server := newTestMessageServer(t, newFakeStore())
		req := new(messagev1.UpdateMessageRequest)
		req.SetMessageId(999)
		req.SetActorUserId(20)
		req.SetContent("new")
		_, err := server.UpdateMessage(t.Context(), req)
		require.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("mod permission bypasses ownership", func(t *testing.T) {
		fake := newFakeStore()
		fake.messages[100] = &model.Message{ID: 100, ChannelID: 10, AuthorID: 20, Content: "old", Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)}
		server := newTestMessageServer(t, fake)
		req := new(messagev1.UpdateMessageRequest)
		req.SetMessageId(100)
		req.SetActorUserId(21)
		req.SetContent("mod-edited")
		req.SetHasPermission(true)
		resp, err := server.UpdateMessage(t.Context(), req)
		require.NoError(t, err)
		require.Equal(t, "mod-edited", resp.GetMessage().GetContent())
	})

	t.Run("update only mentions without content", func(t *testing.T) {
		fake := newFakeStore()
		fake.messages[100] = &model.Message{ID: 100, ChannelID: 10, AuthorID: 20, Content: "old", Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)}
		server := newTestMessageServer(t, fake)
		req := new(messagev1.UpdateMessageRequest)
		req.SetMessageId(100)
		req.SetActorUserId(20)
		mentionList := new(messagev1.MentionList)
		mentionList.SetUserIds([]int64{50})
		req.SetMentions(mentionList)
		resp, err := server.UpdateMessage(t.Context(), req)
		require.NoError(t, err)
		require.Equal(t, "old", resp.GetMessage().GetContent())
		mentionGot := fake.mentions[100]
		require.Len(t, mentionGot, 1)
		require.Equal(t, int64(50), mentionGot[0])
	})
}

func TestListReactionUsersPagination(t *testing.T) {
	fake := newFakeStore()
	fake.messages[100] = &model.Message{ID: 100, ChannelID: 10, AuthorID: 20, Content: "msg", Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)}
	for i := int64(1); i <= 5; i++ {
		fake.reactions[reactionKey{messageID: 100, userID: i, emojiID: 0, emojiName: "🔥"}] = struct{}{}
	}
	server := newTestMessageServer(t, fake)

	t.Run("first page with next cursor", func(t *testing.T) {
		req := new(messagev1.ListReactionUsersRequest)
		req.SetMessageId(100)
		req.SetEmojiId(0)
		req.SetEmojiName("🔥")
		req.SetLimit(3)

		resp, err := server.ListReactionUsers(t.Context(), req)
		require.NoError(t, err)
		require.Len(t, resp.GetUserIds(), 3)
		require.Equal(t, int64(1), resp.GetUserIds()[0])
		require.Equal(t, int64(2), resp.GetUserIds()[1])
		require.Equal(t, int64(3), resp.GetUserIds()[2])
		require.Equal(t, int64(4), resp.GetNextCursor())
	})

	t.Run("second page from cursor", func(t *testing.T) {
		req := new(messagev1.ListReactionUsersRequest)
		req.SetMessageId(100)
		req.SetEmojiId(0)
		req.SetEmojiName("🔥")
		req.SetCursor(3)
		req.SetLimit(3)

		resp, err := server.ListReactionUsers(t.Context(), req)
		require.NoError(t, err)
		require.Len(t, resp.GetUserIds(), 2)
		require.Equal(t, int64(4), resp.GetUserIds()[0])
		require.Equal(t, int64(5), resp.GetUserIds()[1])
		require.Equal(t, int64(0), resp.GetNextCursor())
	})
}

func TestResolveEmojiImageURLs(t *testing.T) {
	fake := newFakeStore()
	fake.messages[100] = &model.Message{ID: 100, ChannelID: 10, AuthorID: 20, Content: "msg", Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)}
	fake.reactions[reactionKey{messageID: 100, userID: 30, emojiID: 42, emojiName: "blob"}] = struct{}{}

	node, _ := snowflake.New()
	svcCtx := &svc.ServiceContext{
		Cfg: config.Config{
			Kafka:           config.KafkaConfig{Topic: "message.events"},
			EmojiCDNBaseURL: "https://cdn.example.com/emojis",
		},
		Store:                fake,
		Snowflake:            node,
		OutboxMaxRetries:     7,
		OutboxPartitionCount: outbox.DefaultPartitionCount,
	}
	srv := New(svcCtx)

	req := new(messagev1.GetMessageRequest)
	req.SetMessageId(100)
	req.SetViewerUserId(30)

	resp, err := srv.GetMessage(t.Context(), req)
	require.NoError(t, err)
	reactions := resp.GetReactions()
	require.Len(t, reactions, 1)
	require.Equal(t, "https://cdn.example.com/emojis/42", reactions[0].GetEmoji().GetImageUrl())
}

func TestResolveEmojiImageURLsTrailingSlash(t *testing.T) {
	fake := newFakeStore()
	fake.messages[100] = &model.Message{ID: 100, ChannelID: 10, AuthorID: 20, Content: "msg", Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)}
	fake.reactions[reactionKey{messageID: 100, userID: 30, emojiID: 1, emojiName: "x"}] = struct{}{}

	node, _ := snowflake.New()
	svcCtx := &svc.ServiceContext{
		Cfg: config.Config{
			Kafka:           config.KafkaConfig{Topic: "message.events"},
			EmojiCDNBaseURL: "https://cdn.example.com/",
		},
		Store:                fake,
		Snowflake:            node,
		OutboxMaxRetries:     7,
		OutboxPartitionCount: outbox.DefaultPartitionCount,
	}
	srv := New(svcCtx)

	req := new(messagev1.GetMessageRequest)
	req.SetMessageId(100)
	req.SetViewerUserId(30)

	resp, err := srv.GetMessage(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, "https://cdn.example.com/1", resp.GetReactions()[0].GetEmoji().GetImageUrl())
}

func newTestMessageServer(t *testing.T, fakeStore store.Store) messagev1.MessageServiceServer {
	t.Helper()

	node, err := snowflake.New()
	require.NoError(t, err)

	return New(&svc.ServiceContext{
		Cfg: config.Config{
			Kafka:  config.KafkaConfig{Topic: "message.events"},
			Outbox: config.OutboxConfig{MaxRetries: 7},
		},
		Store:                fakeStore,
		Snowflake:            node,
		OutboxMaxRetries:     7,
		OutboxPartitionCount: outbox.DefaultPartitionCount,
	})
}

func onlyOutboxEvent(t *testing.T, fake *fakeStore) outbox.Event {
	t.Helper()
	require.Len(t, fake.outbox, 1)
	for _, evt := range fake.outbox {
		return *evt
	}
	panic("unreachable")
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
			summary = &model.ReactionSummary{
				Emoji: model.Emoji{ID: key.emojiID, Name: key.emojiName, ImageKey: fmt.Sprintf("%d", key.emojiID)},
			}
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
