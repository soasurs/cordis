package server

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/cursor"
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
	req.SetContent("hello <@30> <@31>")
	req.SetType(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)
	req.SetFlags(int32(messagev1.MessageFlag_MESSAGE_FLAG_SUPPRESS_NOTIFICATIONS))
	attachment := pbAttachment(101)
	attachment.SetSize(999)
	attachment.SetContentType("application/x-untrusted")
	attachment.SetWidth(999)
	attachment.SetHeight(999)
	attachment.SetBlurhash("client-forged")
	req.SetAttachments([]*messagev1.Attachment{attachment})

	resp, err := server.CreateMessage(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1), resp.GetMessage().GetRevision())
	require.Equal(t, []int64{30, 31}, fakeStore.mentions[resp.GetMessage().GetId()].UserIDs)
	require.Equal(t, 1, fakeStore.listReadyCalls, "create must reload persisted read state instead of constructing it")

	require.Len(t, publisher.records, 2)
	record := publisher.records[0]
	require.Equal(t, "10", string(record.key))
	var envelope eventEnvelope[messagePayload]
	require.NoError(t, json.Unmarshal(record.payload, &envelope))
	require.Equal(t, EventTypeMessageCreated, envelope.Type)
	require.Equal(t, "9001", envelope.Data.GuildID)
	require.Equal(t, strconv.FormatInt(resp.GetMessage().GetId(), 10), envelope.Data.MessageID)
	require.Equal(t, int64(20), resp.GetMessage().GetAuthorId())
	require.Equal(t, int64(20), resp.GetAuthor().GetUserId())
	require.Equal(t, int64(10), resp.GetMessage().GetAttachments()[0].GetSize())
	require.Equal(t, "image/png", resp.GetMessage().GetAttachments()[0].GetContentType())
	require.Equal(t, int32(1), resp.GetMessage().GetAttachments()[0].GetWidth())
	require.Equal(t, int32(1), resp.GetMessage().GetAttachments()[0].GetHeight())
	require.Equal(t, "LEHV6nWB2yk8pyo0adR*.7kCMdnj", resp.GetMessage().GetAttachments()[0].GetBlurhash())
	require.Equal(t, "https://download.example/101", resp.GetMessage().GetAttachments()[0].GetUrl())
	require.Equal(t, int64(9001), resp.GetMessage().GetAttachments()[0].GetUrlExpiresAt())
	require.Equal(t, "20", envelope.Data.Author.UserID)
	require.Equal(t, "https://download.example/101", envelope.Data.Attachments[0].URL)
	require.Equal(t, int64(9001), envelope.Data.Attachments[0].URLExpiresAt)
	require.Equal(t, "LEHV6nWB2yk8pyo0adR*.7kCMdnj", envelope.Data.Attachments[0].Blurhash)
	require.Equal(t, int64(1), envelope.Data.Revision)
	var readEnvelope eventEnvelope[messageReadUpdatedPayload]
	require.NoError(t, json.Unmarshal(publisher.records[1].payload, &readEnvelope))
	require.Equal(t, "20", string(publisher.records[1].key))
	require.Equal(t, EventTypeMessageReadUpdated, readEnvelope.Type)
	require.Equal(t, strconv.FormatInt(resp.GetMessage().GetId(), 10), readEnvelope.Data.LastReadMessageID)
}

func TestCreateMessageIdempotentRetryReturnsSameMessageWithoutSideEffects(t *testing.T) {
	fakeStore := newFakeStore()
	publisher := new(fakePublisher)
	server := newTestMessageServer(t, fakeStore, publisher)

	req := new(messagev1.CreateMessageRequest)
	req.SetChannelId(10)
	req.SetAuthorId(20)
	req.SetContent("hello <@30> <@31>")
	req.SetIdempotencyKey("message-intent-1")

	first, err := server.CreateMessage(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, publisher.records, 2)
	require.Equal(t, []int64{30, 31}, fakeStore.mentions[first.GetMessage().GetId()].UserIDs)
	require.Equal(t, 1, fakeStore.listReadyCalls)

	retry := new(messagev1.CreateMessageRequest)
	retry.SetChannelId(10)
	retry.SetAuthorId(20)
	retry.SetContent("hello <@30> <@31>")
	retry.SetIdempotencyKey("message-intent-1")

	second, err := server.CreateMessage(t.Context(), retry)
	require.NoError(t, err)
	require.Equal(t, first.GetMessage().GetId(), second.GetMessage().GetId())
	require.Equal(t, first.GetMessage().GetCreatedAt(), second.GetMessage().GetCreatedAt())
	require.Len(t, fakeStore.messages, 1)
	require.Len(t, publisher.records, 2)
	require.Equal(t, 1, fakeStore.listReadyCalls)
}

func TestCreateMessageRejectsIdempotencyKeyReuseWithDifferentRequest(t *testing.T) {
	fakeStore := newFakeStore()
	publisher := new(fakePublisher)
	server := newTestMessageServer(t, fakeStore, publisher)

	req := new(messagev1.CreateMessageRequest)
	req.SetChannelId(10)
	req.SetAuthorId(20)
	req.SetContent("hello")
	req.SetIdempotencyKey("message-intent-1")
	_, err := server.CreateMessage(t.Context(), req)
	require.NoError(t, err)

	req.SetContent("different")
	_, err = server.CreateMessage(t.Context(), req)
	require.True(t, rpcerror.Is(err, rpcerror.MessageDomain, rpcerror.MessageIdempotencyKeyReused))
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Len(t, fakeStore.messages, 1)
	require.Len(t, publisher.records, 2)
}

func TestCreateMessageRejectsMalformedIdempotencyKey(t *testing.T) {
	for _, key := range []string{"", " leading", "trailing ", " \t"} {
		t.Run(strconv.Quote(key), func(t *testing.T) {
			req := new(messagev1.CreateMessageRequest)
			req.SetChannelId(10)
			req.SetAuthorId(20)
			req.SetContent("hello")
			req.SetIdempotencyKey(key)

			server := newTestMessageServer(t, newFakeStore(), new(fakePublisher))
			_, err := server.CreateMessage(t.Context(), req)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func TestMessageEventEncodesSnowflakeIDsAsStrings(t *testing.T) {
	message := &model.Message{
		ID: 9007199254740993, ChannelID: 9007199254740994, AuthorID: 9007199254740995,
		ReferencedMessageID: 9007199254740996, ReferencedChannelID: 9007199254740997,
		Revision: 1,
	}
	author := testUserProfile(message.AuthorID)
	events, err := newMessageCreatedEvents(message, author, model.MessageMentions{UserIDs: []int64{9007199254740998}}, messageAudience{guildID: 9007199254740999}, 0)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "9007199254740994", string(events[0].Key))

	var envelope eventEnvelope[map[string]json.RawMessage]
	require.NoError(t, json.Unmarshal(events[0].Payload, &envelope))
	require.Equal(t, `"9007199254740993"`, string(envelope.Data["id"]))
	require.Equal(t, `"9007199254740994"`, string(envelope.Data["channel_id"]))
	var encodedAuthor map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(envelope.Data["author"], &encodedAuthor))
	require.Equal(t, `"9007199254740995"`, string(encodedAuthor["user_id"]))
	require.NotContains(t, envelope.Data, "author_id")
	require.Equal(t, `"9007199254740999"`, string(envelope.Data["guild_id"]))
	require.Equal(t, `"9007199254740996"`, string(envelope.Data["referenced_message_id"]))
	require.Equal(t, `"9007199254740997"`, string(envelope.Data["referenced_channel_id"]))
	require.JSONEq(t, `["9007199254740998"]`, string(envelope.Data["mention_user_ids"]))
}

func TestMessageEventRejectsEmptyDmAudience(t *testing.T) {
	message := &model.Message{ID: 1, ChannelID: 2, AuthorID: 3}
	_, err := newMessageCreatedEvents(message, testUserProfile(message.AuthorID), model.MessageMentions{}, messageAudience{}, 0)
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

func TestCreateMessageLoadsAuthorBeforeWriting(t *testing.T) {
	fakeStore := newFakeStore()
	userClient := newFakeUserClient()
	userClient.getProfileErr = status.Error(codes.Unavailable, "user unavailable")
	server := newTestMessageServerWithClients(t, fakeStore, new(fakePublisher), new(fakeGuildClient), userClient)

	req := new(messagev1.CreateMessageRequest)
	req.SetChannelId(10)
	req.SetAuthorId(20)
	req.SetContent("hello")
	_, err := server.CreateMessage(t.Context(), req)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Empty(t, fakeStore.messages)
	require.Equal(t, []int64{20}, userClient.profileRequests())
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

	mentionUsers := make([]int64, 101)
	for i := range mentionUsers {
		mentionUsers[i] = int64(i + 1)
	}
	require.Equal(t, codes.ResourceExhausted, status.Code(validateMentionsSet(model.MessageMentions{UserIDs: mentionUsers}, 100)))
}

func TestAttachmentUploadLifecycle(t *testing.T) {
	fakeStore := newFakeStore()
	mediaClient := &fakeMediaClient{asset: attachmentAsset(7001, 10, 20)}
	server := newTestMessageServerWithMedia(
		t,
		fakeStore,
		new(fakePublisher),
		new(fakeGuildClient),
		newFakeUserClient(),
		mediaClient,
	)

	createReq := new(messagev1.CreateAttachmentUploadRequest)
	createReq.SetChannelId(10)
	createReq.SetActorUserId(20)
	createReq.SetExpectedSize(123)
	createReq.SetContentType("application/pdf")
	createReq.SetFilename("report.pdf")
	createReq.SetIdempotencyKey("attachment-intent-1")
	createResp, err := server.CreateAttachmentUpload(t.Context(), createReq)
	require.NoError(t, err)
	require.Equal(t, int64(7001), createResp.GetUploadId())
	require.Equal(t, map[string]string{"Content-Type": "application/pdf"}, createResp.GetRequestHeaders())
	require.Equal(t, mediav1.AssetStatus_ASSET_STATUS_CREATED, createResp.GetStatus())
	require.False(t, createResp.GetIdempotentReplay())
	require.Equal(t, int64(20), mediaClient.createRequest.GetActorUserId())
	require.Equal(t, int64(10), mediaClient.createRequest.GetMessageAttachment().GetChannelId())
	require.Equal(t, "report.pdf", mediaClient.createRequest.GetMessageAttachment().GetFilename())
	require.Equal(t, "attachment-intent-1", mediaClient.createRequest.GetIdempotencyKey())

	completeReq := new(messagev1.CompleteAttachmentUploadRequest)
	completeReq.SetChannelId(10)
	completeReq.SetActorUserId(20)
	completeReq.SetUploadId(7001)
	completeResp, err := server.CompleteAttachmentUpload(t.Context(), completeReq)
	require.NoError(t, err)
	require.Equal(t, int64(7001), completeResp.GetAttachment().GetAssetId())
	require.Equal(t, "report.pdf", completeResp.GetAttachment().GetFilename())
	require.Equal(t, int64(123), completeResp.GetAttachment().GetSize())
	require.Equal(t, "application/pdf", completeResp.GetAttachment().GetContentType())
	require.Equal(t, "https://download.example/7001", completeResp.GetAttachment().GetUrl())
	require.Equal(t, int64(9001), completeResp.GetAttachment().GetUrlExpiresAt())
}

func TestCreateMessageRejectsAttachmentOwnedByAnotherUser(t *testing.T) {
	fakeStore := newFakeStore()
	mediaClient := &fakeMediaClient{asset: attachmentAsset(101, 10, 21)}
	server := newTestMessageServerWithMedia(
		t,
		fakeStore,
		new(fakePublisher),
		new(fakeGuildClient),
		newFakeUserClient(),
		mediaClient,
	)
	req := new(messagev1.CreateMessageRequest)
	req.SetChannelId(10)
	req.SetAuthorId(20)
	req.SetContent("hello")
	req.SetAttachments([]*messagev1.Attachment{pbAttachment(101)})

	_, err := server.CreateMessage(t.Context(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.Empty(t, fakeStore.messages)
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
	fakeStore.mentions[100] = model.MessageMentions{UserIDs: []int64{40}}
	publisher := new(fakePublisher)
	server := newTestMessageServer(t, fakeStore, publisher)

	req := new(messagev1.UpdateMessageRequest)
	req.SetMessageId(100)
	req.SetActorUserId(20)
	req.SetContent("edited <@30>")

	resp, err := server.UpdateMessage(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, "edited <@30>", resp.GetMessage().GetContent())
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
	fakeStore.mentions[100] = model.MessageMentions{UserIDs: []int64{30}}
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
		Attachments: []model.Attachment{{AssetID: 7001, Filename: "one.png"}},
	}
	fakeStore.messages[101] = &model.Message{
		ID: 101, ChannelID: 10, AuthorID: 21, Content: "two",
		Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT), Revision: 2,
		Attachments: []model.Attachment{
			{AssetID: 7002, Filename: "two.png"},
			{AssetID: 7001, Filename: "one.png"},
		},
	}
	userClient := newFakeUserClient()
	mediaClient := new(fakeMediaClient)
	server := newTestMessageServerWithMedia(
		t,
		fakeStore,
		new(fakePublisher),
		new(fakeGuildClient),
		userClient,
		mediaClient,
	)

	getReq := new(messagev1.GetMessageRequest)
	getReq.SetMessageId(101)
	getReq.SetUserId(20)
	getResp, err := server.GetMessage(t.Context(), getReq)
	require.NoError(t, err)
	require.Equal(t, int64(2), getResp.GetMessage().GetRevision())
	require.Equal(t, int64(21), getResp.GetMessage().GetAuthorId())
	require.Equal(t, int64(21), getResp.GetAuthor().GetUserId())
	require.Equal(t, "https://download.example/7002", getResp.GetMessage().GetAttachments()[0].GetUrl())

	listReq := new(messagev1.ListMessagesRequest)
	listReq.SetChannelId(10)
	listReq.SetUserId(20)
	listReq.SetBefore(200)
	listResp, err := server.ListMessages(t.Context(), listReq)
	require.NoError(t, err)
	require.Len(t, listResp.GetMessages(), 2)
	require.Equal(t, int64(101), listResp.GetMessages()[0].GetId())
	require.Equal(t, int64(21), listResp.GetMessages()[0].GetAuthorId())
	require.Equal(t, int64(20), listResp.GetMessages()[1].GetAuthorId())
	require.Equal(t, int64(100), listResp.GetBeforeCursor())
	require.Equal(t, int64(101), listResp.GetAfterCursor())
	require.Equal(t, []int64{21}, userClient.profileRequests())
	require.Empty(t, userClient.batchRequests(), "list must not load author profiles")
	require.Len(t, mediaClient.batchRequests, 2)
	require.Equal(t, []int64{7002, 7001}, mediaClient.batchRequests[0].GetAssetIds())
	require.Equal(t, []int64{7002, 7001}, mediaClient.batchRequests[1].GetAssetIds())
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestMessageServer(t, newFakeStore(), new(fakePublisher))
			_, err := server.CreateMessage(t.Context(), tt.req())
			require.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func TestCreateMessageMentionLimitExceeded(t *testing.T) {
	req := new(messagev1.CreateMessageRequest)
	req.SetChannelId(1)
	req.SetAuthorId(1)
	var content strings.Builder
	for i := 1; i <= 101; i++ {
		content.WriteString("<@")
		content.WriteString(strconv.Itoa(i))
		content.WriteString("> ")
	}
	req.SetContent(content.String())

	server := newTestMessageServer(t, newFakeStore(), new(fakePublisher))
	_, err := server.CreateMessage(t.Context(), req)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
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
	return newTestMessageServerWithClients(t, fakeStore, publisher, guildClient, newFakeUserClient())
}

func newTestMessageServerWithClients(
	t *testing.T,
	fakeStore store.Store,
	publisher svc.EventPublisher,
	guildClient guildv1.GuildServiceClient,
	userClient userv1.UserServiceClient,
) messagev1.MessageServiceServer {
	return newTestMessageServerWithMedia(
		t,
		fakeStore,
		publisher,
		guildClient,
		userClient,
		&fakeMediaClient{},
	)
}

func newTestMessageServerWithMedia(
	t *testing.T,
	fakeStore store.Store,
	publisher svc.EventPublisher,
	guildClient guildv1.GuildServiceClient,
	userClient userv1.UserServiceClient,
	mediaClient mediav1.MediaServiceClient,
) messagev1.MessageServiceServer {
	t.Helper()
	node, err := snowflake.New()
	require.NoError(t, err)
	codec, err := cursor.NewCodec("test-cursor-secret-at-least-32-bytes!")
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
		Cursors:     codec,
		Publisher:   publisher,
		GuildClient: guildClient,
		UserClient:  userClient,
		MediaClient: mediaClient,
	})
}

func testUserProfile(userID int64) *userv1.UserProfile {
	profile := new(userv1.UserProfile)
	profile.SetUserId(userID)
	profile.SetName("User " + strconv.FormatInt(userID, 10))
	profile.SetUsername("user_" + strconv.FormatInt(userID, 10))
	profile.SetAvatarAssetId(userID + 1000)
	profile.SetCreatedAt(1)
	profile.SetUpdatedAt(2)
	return profile
}
