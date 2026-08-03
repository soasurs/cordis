package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/message/v1/config"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
	"github.com/soasurs/cordis/services/message/v1/internal/svc"
)

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
	require.Len(t, fakeStore.readStateOutbox, 1)
	record := fakeStore.readStateOutbox[0]
	require.Equal(t, "1:10", string(record.Key))
	require.Equal(t, EventTypeMessageReadUpdated, record.EventType)
	var envelope eventEnvelope[messageReadUpdatedPayload]
	require.NoError(t, json.Unmarshal(record.Payload, &envelope))
	require.Equal(t, EventTypeMessageReadUpdated, envelope.Type)
	require.NotZero(t, envelope.StreamSequence)
	require.Equal(t, "50", envelope.Data.LastReadMessageID)

	resp, err = server.AckMessage(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int64(50), resp.GetReadState().GetLastReadMessageId())
	require.Len(t, fakeStore.readStateOutbox, 1, "a no-op ack must not enqueue another event")
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
	fakeStore.mentions[52] = model.MessageMentions{UserIDs: []int64{1}}
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
