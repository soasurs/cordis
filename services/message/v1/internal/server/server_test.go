package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strconv"
	"sync"
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
	require.Equal(t, 1, fakeStore.listReadyCalls, "create must reload persisted read state instead of constructing it")

	require.Len(t, publisher.records, 2)
	record := publisher.records[0]
	require.Equal(t, "10", string(record.key))
	var envelope eventEnvelope[messagePayload]
	require.NoError(t, json.Unmarshal(record.payload, &envelope))
	require.Equal(t, EventTypeMessageCreated, envelope.Type)
	require.Equal(t, "9001", envelope.Data.GuildID)
	require.Equal(t, strconv.FormatInt(resp.GetMessage().GetId(), 10), envelope.Data.MessageID)
	require.Equal(t, int64(1), envelope.Data.Revision)
	var readEnvelope eventEnvelope[messageReadUpdatedPayload]
	require.NoError(t, json.Unmarshal(publisher.records[1].payload, &readEnvelope))
	require.Equal(t, "20", string(publisher.records[1].key))
	require.Equal(t, EventTypeMessageReadUpdated, readEnvelope.Type)
	require.Equal(t, strconv.FormatInt(resp.GetMessage().GetId(), 10), readEnvelope.Data.LastReadMessageID)
}

func TestMessageEventEncodesSnowflakeIDsAsStrings(t *testing.T) {
	message := &model.Message{
		ID: 9007199254740993, ChannelID: 9007199254740994, AuthorID: 9007199254740995,
		ReferencedMessageID: 9007199254740996, ReferencedChannelID: 9007199254740997,
		Revision: 1,
	}
	events, err := newMessageCreatedEvents(message, []int64{9007199254740998}, messageAudience{guildID: 9007199254740999}, 0)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "9007199254740994", string(events[0].Key))

	var envelope eventEnvelope[map[string]json.RawMessage]
	require.NoError(t, json.Unmarshal(events[0].Payload, &envelope))
	require.Equal(t, `"9007199254740993"`, string(envelope.Data["id"]))
	require.Equal(t, `"9007199254740994"`, string(envelope.Data["channel_id"]))
	require.Equal(t, `"9007199254740995"`, string(envelope.Data["author_id"]))
	require.Equal(t, `"9007199254740999"`, string(envelope.Data["guild_id"]))
	require.Equal(t, `"9007199254740996"`, string(envelope.Data["referenced_message_id"]))
	require.Equal(t, `"9007199254740997"`, string(envelope.Data["referenced_channel_id"]))
	require.JSONEq(t, `["9007199254740998"]`, string(envelope.Data["mention_user_ids"]))
}

func TestMessageEventRejectsEmptyDmAudience(t *testing.T) {
	message := &model.Message{ID: 1, ChannelID: 2, AuthorID: 3}
	_, err := newMessageCreatedEvents(message, nil, messageAudience{}, 0)
	require.Error(t, err)
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
	require.Len(t, publisher.records, 2)
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

func TestMessageResourceLimits(t *testing.T) {
	attachments := make([]model.Attachment, 11)
	require.Equal(t, codes.ResourceExhausted, status.Code(validateAttachments(attachments, 10)))

	mentions := make([]int64, 101)
	for i := range mentions {
		mentions[i] = int64(i + 1)
	}
	require.Equal(t, codes.ResourceExhausted, status.Code(validateMentionUserIDs(mentions, 100)))
	require.Equal(t, codes.InvalidArgument, status.Code(validateMentionUserIDs([]int64{1, 1}, 100)))
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
	fakeStore.mentions[100] = []int64{40}
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
	require.Equal(t, []string{"40"}, envelope.Data.PreviousMentionUserIDs)
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
	fakeStore.messages[99] = &model.Message{
		ID: 99, ChannelID: 10, AuthorID: 20, Content: "previous",
		Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT), Revision: 1,
	}
	fakeStore.mentions[100] = []int64{30}
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
	require.Equal(t, "99", envelope.Data.LastMessageID)
	require.Equal(t, []string{"30"}, envelope.Data.MentionUserIDs)
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
	mu                    sync.Mutex
	allowManageMessages   bool
	denyAll               bool
	channelType           guildv1.GuildChannelType
	authorizeRequests     []*guildv1.AuthorizeGuildChannelRequest
	visibleTextChannelIDs []int64
}

func (f *fakeGuildClient) GetUserGuildChannelVisibility(
	_ context.Context,
	req *guildv1.GetUserGuildChannelVisibilityRequest,
	_ ...grpc.CallOption,
) (*guildv1.GetUserGuildChannelVisibilityResponse, error) {
	visibility := new(guildv1.GuildChannelVisibility)
	visibility.SetGuildId(req.GetGuildId())
	visibility.SetAccessRevision(1)
	visibility.SetVisibleTextChannelIds(f.visibleTextChannelIDs)
	resp := new(guildv1.GetUserGuildChannelVisibilityResponse)
	resp.SetVisibility(visibility)
	return resp, nil
}

func (f *fakeGuildClient) AuthorizeGuildChannel(
	_ context.Context,
	req *guildv1.AuthorizeGuildChannelRequest,
	_ ...grpc.CallOption,
) (*guildv1.AuthorizeGuildChannelResponse, error) {
	f.mu.Lock()
	f.authorizeRequests = append(f.authorizeRequests, req)
	f.mu.Unlock()
	resp := new(guildv1.AuthorizeGuildChannelResponse)
	resp.SetAllowed(!f.denyAll && (req.GetPermission()&permissionManageMessages == 0 || f.allowManageMessages))
	resp.SetGuildId(9001)
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

type fakeReadStatesLimiter struct {
	weights  []int64
	releases int
}

func (l *fakeReadStatesLimiter) Acquire(_ context.Context, weight int64) (func(), error) {
	l.weights = append(l.weights, weight)
	return func() { l.releases++ }, nil
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
	messages        map[int64]*model.Message
	mentions        map[int64][]int64
	dmChannels      map[int64]*model.DmChannel
	readStates      map[int64]map[int64]int64 // userID -> channelID -> lastReadID
	listReadyCalls  int
	readyBatchSizes []int
	transactErr     error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		messages:   make(map[int64]*model.Message),
		mentions:   make(map[int64][]int64),
		dmChannels: make(map[int64]*model.DmChannel),
		readStates: make(map[int64]map[int64]int64),
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

func (s *fakeStore) CreateDmChannel(_ context.Context, channel *model.DmChannel) error {
	for _, existing := range s.dmChannels {
		if existing.UserLo == channel.UserLo && existing.UserHi == channel.UserHi {
			return sql.ErrNoRows
		}
	}
	value := *channel
	s.dmChannels[channel.ID] = &value
	return nil
}

func (s *fakeStore) GetDmChannel(_ context.Context, channelID int64) (*model.DmChannel, error) {
	channel, ok := s.dmChannels[channelID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	value := *channel
	return &value, nil
}

func (s *fakeStore) GetDmChannelByPair(_ context.Context, userLo, userHi int64) (*model.DmChannel, error) {
	for _, channel := range s.dmChannels {
		if channel.UserLo == userLo && channel.UserHi == userHi {
			value := *channel
			return &value, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (s *fakeStore) ListDmChannels(_ context.Context, params store.ListDmChannelsParams) ([]*model.DmChannel, error) {
	var channels []*model.DmChannel
	for _, channel := range s.dmChannels {
		if channel.UserLo != params.UserID && channel.UserHi != params.UserID {
			continue
		}
		if params.BeforeID != 0 && channel.ID >= params.BeforeID {
			continue
		}
		value := *channel
		channels = append(channels, &value)
	}
	sort.Slice(channels, func(i, j int) bool { return channels[i].ID > channels[j].ID })
	if len(channels) > params.Limit {
		channels = channels[:params.Limit]
	}
	return channels, nil
}

func (s *fakeStore) ListAllDmChannels(_ context.Context, userID int64) ([]*model.DmChannel, error) {
	var channels []*model.DmChannel
	for _, channel := range s.dmChannels {
		if channel.UserLo != userID && channel.UserHi != userID {
			continue
		}
		value := *channel
		channels = append(channels, &value)
	}
	sort.Slice(channels, func(i, j int) bool { return channels[i].ID > channels[j].ID })
	return channels, nil
}

func (s *fakeStore) AckMessage(_ context.Context, userID, channelID, messageID int64) (bool, error) {
	message, ok := s.messages[messageID]
	if !ok || message.ChannelID != channelID {
		return false, sql.ErrNoRows
	}
	if s.readStates[userID] == nil {
		s.readStates[userID] = make(map[int64]int64)
	}
	if current, ok := s.readStates[userID][channelID]; !ok || messageID > current {
		s.readStates[userID][channelID] = messageID
		return true, nil
	}
	return false, nil
}

func (s *fakeStore) ListReadyChannelReadStates(_ context.Context, userID int64, channelIDs []int64) ([]*model.ChannelReadState, error) {
	s.listReadyCalls++
	s.readyBatchSizes = append(s.readyBatchSizes, len(channelIDs))
	var states []*model.ChannelReadState
	byChannel := s.readStates[userID]
	for _, channelID := range channelIDs {
		lastReadID := int64(0)
		if byChannel != nil {
			if v, ok := byChannel[channelID]; ok {
				lastReadID = v
			}
		}
		state := &model.ChannelReadState{
			UserID:            userID,
			ChannelID:         channelID,
			LastReadMessageID: lastReadID,
		}
		for _, message := range s.messages {
			if message.ChannelID != channelID || message.DeletedAt != 0 {
				continue
			}
			state.LastMessageID = max(state.LastMessageID, message.ID)
			if message.ID <= lastReadID {
				continue
			}
			for _, mentionedID := range s.mentions[message.ID] {
				if mentionedID == userID {
					state.MentionCount++
					break
				}
			}
		}
		states = append(states, state)
	}
	return states, nil
}

func (s *fakeStore) GetLastMessageID(_ context.Context, channelID int64) (int64, error) {
	var lastMessageID int64
	for _, message := range s.messages {
		if message.ChannelID == channelID && message.DeletedAt == 0 {
			lastMessageID = max(lastMessageID, message.ID)
		}
	}
	return lastMessageID, nil
}

func TestAckMessageSuccess(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.messages[50] = &model.Message{ID: 50, ChannelID: 10, AuthorID: 2}
	fakeGuild := &fakeGuildClient{}
	publisher := new(fakePublisher)
	server := newTestMessageServerWithGuild(t, fakeStore, publisher, fakeGuild)

	req := new(messagev1.AckMessageRequest)
	req.SetUserId(1)
	req.SetChannelId(10)
	req.SetMessageId(50)

	resp, err := server.AckMessage(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int64(10), resp.GetReadState().GetChannelId())
	require.Equal(t, int64(50), resp.GetReadState().GetLastMessageId())
	require.Equal(t, int64(50), resp.GetReadState().GetLastReadMessageId())

	require.Equal(t, int64(50), fakeStore.readStates[1][10])
	record := publisher.onlyRecord(t)
	require.Equal(t, "1", string(record.key))
	var envelope eventEnvelope[messageReadUpdatedPayload]
	require.NoError(t, json.Unmarshal(record.payload, &envelope))
	require.Equal(t, EventTypeMessageReadUpdated, envelope.Type)
	require.Equal(t, "50", envelope.Data.LastReadMessageID)

	resp, err = server.AckMessage(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int64(50), resp.GetReadState().GetLastReadMessageId())
	require.Len(t, publisher.records, 1, "a no-op ack must not publish another event")
}

func TestAckMessageHidesMissingOrMismatchedMessage(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.messages[50] = &model.Message{ID: 50, ChannelID: 20, AuthorID: 2}
	server := newTestMessageServerWithGuild(t, fakeStore, new(fakePublisher), new(fakeGuildClient))

	for _, messageID := range []int64{50, 999} {
		req := new(messagev1.AckMessageRequest)
		req.SetUserId(1)
		req.SetChannelId(10)
		req.SetMessageId(messageID)

		_, err := server.AckMessage(t.Context(), req)
		require.Equal(t, codes.NotFound, status.Code(err))
		require.True(t, rpcerror.Is(err, rpcerror.MessageDomain, rpcerror.MessageNotFound))
	}
	require.Empty(t, fakeStore.readStates)
}

func TestAckMessagePermissionDenied(t *testing.T) {
	server := newTestMessageServerWithGuild(
		t,
		newFakeStore(),
		new(fakePublisher),
		&fakeGuildClient{denyAll: true},
	)

	req := new(messagev1.AckMessageRequest)
	req.SetUserId(1)
	req.SetChannelId(10)
	req.SetMessageId(50)

	_, err := server.AckMessage(t.Context(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestGetUserReadyStateIncludesGuildChannelsAndAllDMs(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.readStates[1] = map[int64]int64{10: 50}
	fakeStore.messages[51] = &model.Message{ID: 51, ChannelID: 10, AuthorID: 2}
	fakeStore.messages[52] = &model.Message{ID: 52, ChannelID: 20, AuthorID: 2}
	fakeStore.mentions[52] = []int64{1}
	fakeStore.dmChannels[20] = &model.DmChannel{ID: 20, UserLo: 1, UserHi: 2}
	fakeStore.dmChannels[30] = &model.DmChannel{ID: 30, UserLo: 2, UserHi: 3}
	server := newTestMessageServer(t, fakeStore, new(fakePublisher))

	req := new(messagev1.GetUserReadyStateRequest)
	req.SetUserId(1)
	req.SetGuildChannelIds([]int64{10, 10})
	resp, err := server.GetUserReadyState(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetDmChannels(), 1)
	require.Equal(t, int64(20), resp.GetDmChannels()[0].GetId())
	require.Len(t, resp.GetReadStates(), 2)
	require.Equal(t, int64(51), resp.GetReadStates()[0].GetLastMessageId())
	require.Equal(t, int64(50), resp.GetReadStates()[0].GetLastReadMessageId())
	require.Equal(t, int64(52), resp.GetReadStates()[1].GetLastMessageId())
	require.Equal(t, int32(1), resp.GetReadStates()[1].GetMentionCount())
}

func TestGetUserReadyStateRejectsInvalidRequest(t *testing.T) {
	server := newTestMessageServer(t, newFakeStore(), new(fakePublisher))

	req := new(messagev1.GetUserReadyStateRequest)
	_, err := server.GetUserReadyState(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	req.SetUserId(1)
	req.SetGuildChannelIds([]int64{0})
	_, err = server.GetUserReadyState(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.MessageDomain, rpcerror.MessageInvalidRequest))
}

func TestGetUserReadyStateBatchesReadStateQueriesAtLimiterCapacity(t *testing.T) {
	fakeStore := newFakeStore()
	limiter := new(fakeReadStatesLimiter)
	server := New(&svc.ServiceContext{
		Cfg:               config.Config{ReadStates: config.ReadStatesConfig{MaxConcurrentChannels: 2}},
		Store:             fakeStore,
		ReadStatesLimiter: limiter,
	})

	req := new(messagev1.GetUserReadyStateRequest)
	req.SetUserId(1)
	req.SetGuildChannelIds([]int64{10, 11, 12, 13, 14})
	resp, err := server.GetUserReadyState(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetReadStates(), 5)
	require.Equal(t, []int{2, 2, 1}, fakeStore.readyBatchSizes)
	require.Equal(t, []int64{2, 2, 1}, limiter.weights)
	require.Equal(t, 3, limiter.releases)
}

func TestGetReadStatesUsesGuildAndDmScopes(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.messages[51] = &model.Message{ID: 51, ChannelID: 10, AuthorID: 2}
	fakeStore.messages[52] = &model.Message{ID: 52, ChannelID: 20, AuthorID: 2}
	fakeStore.dmChannels[20] = &model.DmChannel{ID: 20, UserLo: 1, UserHi: 2}
	guild := &fakeGuildClient{visibleTextChannelIDs: []int64{10}}
	server := newTestMessageServerWithGuild(t, fakeStore, new(fakePublisher), guild)

	guildReq := new(messagev1.GetReadStatesRequest)
	guildReq.SetUserId(1)
	guildReq.SetScope(messagev1.ReadStateScopeType_READ_STATE_SCOPE_TYPE_GUILD)
	guildReq.SetGuildId(9001)
	guildResp, err := server.GetReadStates(t.Context(), guildReq)
	require.NoError(t, err)
	require.Empty(t, guildResp.GetDmChannels())
	require.Equal(t, int64(10), guildResp.GetReadStates()[0].GetChannelId())

	dmReq := new(messagev1.GetReadStatesRequest)
	dmReq.SetUserId(1)
	dmReq.SetScope(messagev1.ReadStateScopeType_READ_STATE_SCOPE_TYPE_ALL_DMS)
	dmResp, err := server.GetReadStates(t.Context(), dmReq)
	require.NoError(t, err)
	require.Equal(t, int64(20), dmResp.GetDmChannels()[0].GetId())
	require.Equal(t, int64(20), dmResp.GetReadStates()[0].GetChannelId())
}

func cloneMessage(message *model.Message) *model.Message {
	clone := *message
	clone.Attachments = append([]model.Attachment(nil), message.Attachments...)
	return &clone
}
