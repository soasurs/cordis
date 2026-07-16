package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/message/v1/config"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
	"github.com/soasurs/cordis/services/message/v1/internal/svc"
)

func TestCreateMessagePublishesEvent(t *testing.T) {
	fakeStore := newFakeStore()
	publisher := new(fakePublisher)
	server := newTestMessageServer(t, fakeStore, publisher)

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
	require.Equal(t, int64(1), resp.GetMessage().GetRevision())
	require.Equal(t, []int64{30, 31}, fakeStore.mentions[resp.GetMessage().GetId()])

	record := publisher.onlyRecord(t)
	require.Equal(t, "10", string(record.key))
	var envelope eventEnvelope[messagePayload]
	require.NoError(t, json.Unmarshal(record.payload, &envelope))
	require.Equal(t, EventTypeMessageCreated, envelope.Type)
	require.Equal(t, strconv.FormatInt(resp.GetMessage().GetId(), 10), envelope.Data.MessageID)
	require.Equal(t, int64(1), envelope.Data.Revision)
}

func TestMessageEventEncodesSnowflakeIDsAsStrings(t *testing.T) {
	message := &model.Message{
		ID: 9007199254740993, ChannelID: 9007199254740994, AuthorID: 9007199254740995,
		ReferencedMessageID: 9007199254740996, ReferencedChannelID: 9007199254740997,
		Revision: 1,
	}
	event, err := newMessageCreatedEvent(message, []int64{9007199254740998})
	require.NoError(t, err)

	var envelope eventEnvelope[map[string]json.RawMessage]
	require.NoError(t, json.Unmarshal(event.Payload, &envelope))
	require.Equal(t, `"9007199254740993"`, string(envelope.Data["id"]))
	require.Equal(t, `"9007199254740994"`, string(envelope.Data["channel_id"]))
	require.Equal(t, `"9007199254740995"`, string(envelope.Data["author_id"]))
	require.Equal(t, `"9007199254740996"`, string(envelope.Data["referenced_message_id"]))
	require.Equal(t, `"9007199254740997"`, string(envelope.Data["referenced_channel_id"]))
	require.JSONEq(t, `["9007199254740998"]`, string(envelope.Data["mention_user_ids"]))
}

func TestCreateMessagePublishFailureIsBestEffort(t *testing.T) {
	fakeStore := newFakeStore()
	publisher := &fakePublisher{err: errors.New("kafka unavailable")}
	server := newTestMessageServer(t, fakeStore, publisher)

	req := new(messagev1.CreateMessageRequest)
	req.SetChannelId(10)
	req.SetAuthorId(20)
	req.SetContent("hello")

	resp, err := server.CreateMessage(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, resp.GetMessage())
	require.Len(t, publisher.records, 1)
}

func TestCreateMessageTransactionFailureDoesNotPublish(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.transactErr = errors.New("commit failed")
	publisher := new(fakePublisher)
	server := newTestMessageServer(t, fakeStore, publisher)

	req := new(messagev1.CreateMessageRequest)
	req.SetChannelId(10)
	req.SetAuthorId(20)
	req.SetContent("hello")

	_, err := server.CreateMessage(t.Context(), req)
	require.Error(t, err)
	require.Empty(t, publisher.records)
}

func TestCreateMessageRejectsVoiceChannel(t *testing.T) {
	server := newTestMessageServerWithGuild(
		t,
		newFakeStore(),
		new(fakePublisher),
		&fakeGuildClient{channelType: guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_VOICE},
	)
	req := new(messagev1.CreateMessageRequest)
	req.SetChannelId(10)
	req.SetAuthorId(20)
	req.SetContent("hello")

	_, err := server.CreateMessage(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestCreateReplyValidatesReferencedChannel(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.messages[100] = &model.Message{
		ID: 100, ChannelID: 10, AuthorID: 20, Content: "root",
		Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT), Revision: 1,
	}
	server := newTestMessageServer(t, fakeStore, new(fakePublisher))

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

func TestUpdateMessageIncrementsRevisionAndPublishesEvent(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.messages[100] = &model.Message{
		ID: 100, ChannelID: 10, AuthorID: 20, Content: "old",
		Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT), Revision: 1,
	}
	publisher := new(fakePublisher)
	server := newTestMessageServer(t, fakeStore, publisher)

	req := new(messagev1.UpdateMessageRequest)
	req.SetMessageId(100)
	req.SetActorUserId(20)
	req.SetContent("edited")
	mentionList := new(messagev1.MentionList)
	mentionList.SetUserIds([]int64{30})
	req.SetMentions(mentionList)

	resp, err := server.UpdateMessage(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, "edited", resp.GetMessage().GetContent())
	require.Equal(t, int64(2), resp.GetMessage().GetRevision())

	var envelope eventEnvelope[messagePayload]
	require.NoError(t, json.Unmarshal(publisher.onlyRecord(t).payload, &envelope))
	require.Equal(t, EventTypeMessageUpdated, envelope.Type)
	require.Equal(t, int64(2), envelope.Data.Revision)
	require.Equal(t, []string{"30"}, envelope.Data.MentionUserIDs)
}

func TestUpdateMessagePermissionDenied(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.messages[100] = &model.Message{
		ID: 100, ChannelID: 10, AuthorID: 20, Content: "old",
		Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT), Revision: 1,
	}
	server := newTestMessageServer(t, fakeStore, new(fakePublisher))

	req := new(messagev1.UpdateMessageRequest)
	req.SetMessageId(100)
	req.SetActorUserId(21)
	req.SetContent("edited")

	_, err := server.UpdateMessage(t.Context(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.MessageDomain, rpcerror.MessagePermissionDenied))
}

func TestUpdateMessageAllowsGuildModerator(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.messages[100] = &model.Message{
		ID: 100, ChannelID: 10, AuthorID: 20, Content: "old",
		Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT), Revision: 1,
	}
	server := newTestMessageServerWithGuild(t, fakeStore, new(fakePublisher), &fakeGuildClient{allowManageMessages: true})

	req := new(messagev1.UpdateMessageRequest)
	req.SetMessageId(100)
	req.SetActorUserId(21)
	req.SetContent("moderated")
	resp, err := server.UpdateMessage(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, "moderated", resp.GetMessage().GetContent())
}

func TestCreateMessageRequiresSendPermission(t *testing.T) {
	server := newTestMessageServerWithGuild(t, newFakeStore(), new(fakePublisher), &fakeGuildClient{denyAll: true})
	req := new(messagev1.CreateMessageRequest)
	req.SetChannelId(10)
	req.SetAuthorId(20)
	req.SetContent("hello")
	_, err := server.CreateMessage(t.Context(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestDeleteMessageIncrementsRevisionAndPublishesEvent(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.messages[100] = &model.Message{
		ID: 100, ChannelID: 10, AuthorID: 20, Content: "hello",
		Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT), Revision: 2,
	}
	publisher := new(fakePublisher)
	server := newTestMessageServer(t, fakeStore, publisher)

	req := new(messagev1.DeleteMessageRequest)
	req.SetMessageId(100)
	req.SetActorUserId(20)

	resp, err := server.DeleteMessage(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Equal(t, int64(3), fakeStore.messages[100].Revision)

	var envelope eventEnvelope[messageDeletedPayload]
	require.NoError(t, json.Unmarshal(publisher.onlyRecord(t).payload, &envelope))
	require.Equal(t, EventTypeMessageDeleted, envelope.Type)
	require.Equal(t, int64(3), envelope.Data.Revision)
	require.NotZero(t, envelope.Data.DeletedAt)
}

func TestGetAndListMessages(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.messages[100] = &model.Message{
		ID: 100, ChannelID: 10, AuthorID: 20, Content: "one",
		Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT), Revision: 1,
	}
	fakeStore.messages[101] = &model.Message{
		ID: 101, ChannelID: 10, AuthorID: 20, Content: "two",
		Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT), Revision: 2,
	}
	server := newTestMessageServer(t, fakeStore, new(fakePublisher))

	getReq := new(messagev1.GetMessageRequest)
	getReq.SetMessageId(101)
	getReq.SetUserId(20)
	getResp, err := server.GetMessage(t.Context(), getReq)
	require.NoError(t, err)
	require.Equal(t, int64(2), getResp.GetMessage().GetRevision())

	listReq := new(messagev1.ListMessagesRequest)
	listReq.SetChannelId(10)
	listReq.SetUserId(20)
	listReq.SetBefore(200)
	listResp, err := server.ListMessages(t.Context(), listReq)
	require.NoError(t, err)
	require.Len(t, listResp.GetMessages(), 2)
	require.Equal(t, int64(101), listResp.GetMessages()[0].GetId())
	require.Equal(t, int64(100), listResp.GetBeforeCursor())
	require.Equal(t, int64(101), listResp.GetAfterCursor())
}

func TestCreateMessageValidation(t *testing.T) {
	tests := []struct {
		name string
		req  func() *messagev1.CreateMessageRequest
	}{
		{
			name: "missing channel id",
			req: func() *messagev1.CreateMessageRequest {
				req := new(messagev1.CreateMessageRequest)
				req.SetAuthorId(1)
				req.SetContent("hi")
				return req
			},
		},
		{
			name: "missing author id",
			req: func() *messagev1.CreateMessageRequest {
				req := new(messagev1.CreateMessageRequest)
				req.SetChannelId(1)
				req.SetContent("hi")
				return req
			},
		},
		{
			name: "empty content no attachments",
			req: func() *messagev1.CreateMessageRequest {
				req := new(messagev1.CreateMessageRequest)
				req.SetChannelId(1)
				req.SetAuthorId(1)
				return req
			},
		},
		{
			name: "invalid mention user id",
			req: func() *messagev1.CreateMessageRequest {
				req := new(messagev1.CreateMessageRequest)
				req.SetChannelId(1)
				req.SetAuthorId(1)
				req.SetContent("hi")
				req.SetMentionUserIds([]int64{-1})
				return req
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestMessageServer(t, newFakeStore(), new(fakePublisher))
			_, err := server.CreateMessage(t.Context(), tt.req())
			require.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func newTestMessageServer(t *testing.T, fakeStore store.Store, publisher svc.EventPublisher) messagev1.MessageServiceServer {
	return newTestMessageServerWithGuild(t, fakeStore, publisher, &fakeGuildClient{})
}

func newTestMessageServerWithGuild(
	t *testing.T,
	fakeStore store.Store,
	publisher svc.EventPublisher,
	guildClient guildv1.GuildServiceClient,
) messagev1.MessageServiceServer {
	t.Helper()
	node, err := snowflake.New()
	require.NoError(t, err)
	return New(&svc.ServiceContext{
		Cfg: config.Config{
			Kafka: config.KafkaConfig{
				Topic:            "cordis.message.events.v1",
				PublishTimeoutMs: 100,
			},
		},
		Store:       fakeStore,
		Snowflake:   node,
		Publisher:   publisher,
		GuildClient: guildClient,
	})
}

type fakeGuildClient struct {
	guildv1.GuildServiceClient
	allowManageMessages bool
	denyAll             bool
	channelType         guildv1.GuildChannelType
}

func (f *fakeGuildClient) AuthorizeGuildChannel(
	_ context.Context,
	req *guildv1.AuthorizeGuildChannelRequest,
	_ ...grpc.CallOption,
) (*guildv1.AuthorizeGuildChannelResponse, error) {
	resp := new(guildv1.AuthorizeGuildChannelResponse)
	resp.SetAllowed(!f.denyAll && (req.GetPermission()&permissionManageMessages == 0 || f.allowManageMessages))
	resp.SetPermissions(permissionViewChannel | permissionSendMessages)
	resp.SetChannelType(f.channelType)
	return resp, nil
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

type publishedRecord struct {
	key     []byte
	payload []byte
}

type fakePublisher struct {
	records []publishedRecord
	err     error
}

func (p *fakePublisher) Publish(_ context.Context, key, payload []byte) error {
	p.records = append(p.records, publishedRecord{
		key:     append([]byte(nil), key...),
		payload: append([]byte(nil), payload...),
	})
	return p.err
}

func (p *fakePublisher) onlyRecord(t *testing.T) publishedRecord {
	t.Helper()
	require.Len(t, p.records, 1)
	return p.records[0]
}

type fakeStore struct {
	messages    map[int64]*model.Message
	mentions    map[int64][]int64
	transactErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		messages: make(map[int64]*model.Message),
		mentions: make(map[int64][]int64),
	}
}

func (s *fakeStore) Transact(_ context.Context, fn func(txStore store.Store) error) error {
	if err := fn(s); err != nil {
		return err
	}
	return s.transactErr
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
		Revision:            1,
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
	message.Revision++
	return cloneMessage(message), nil
}

func (s *fakeStore) DeleteMessage(_ context.Context, messageID, actorUserID int64, hasModPermission bool) (*model.Message, error) {
	message, ok := s.messages[messageID]
	if !ok || message.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	if !hasModPermission && message.AuthorID != actorUserID {
		return nil, store.ErrPermissionDenied
	}
	message.DeletedAt = 3
	message.UpdatedAt = 3
	message.Revision++
	return cloneMessage(message), nil
}

func (s *fakeStore) ReplaceMessageMentions(_ context.Context, messageID int64, userIDs []int64) error {
	s.mentions[messageID] = append([]int64(nil), userIDs...)
	slices.Sort(s.mentions[messageID])
	return nil
}

func (s *fakeStore) ListMentionUserIDs(_ context.Context, messageID int64) ([]int64, error) {
	return append([]int64(nil), s.mentions[messageID]...), nil
}

func cloneMessage(message *model.Message) *model.Message {
	clone := *message
	clone.Attachments = append([]model.Attachment(nil), message.Attachments...)
	return &clone
}
