package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/message/v1/config"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
	"github.com/soasurs/cordis/services/message/v1/internal/svc"
)

// fakeUserClient stores one relationship row per direction, mirroring the
// user service's directed model.
type fakeUserClient struct {
	userv1.UserServiceClient
	relationships map[[2]int64]int32
}

func newFakeUserClient() *fakeUserClient {
	return &fakeUserClient{relationships: make(map[[2]int64]int32)}
}

func (f *fakeUserClient) set(userID, targetID int64, relationshipType userv1.RelationshipType) {
	f.relationships[[2]int64{userID, targetID}] = int32(relationshipType)
}

func (f *fakeUserClient) CheckRelationships(_ context.Context, req *userv1.CheckRelationshipsRequest, _ ...grpc.CallOption) (*userv1.CheckRelationshipsResponse, error) {
	var values []*userv1.Relationship
	appendRow := func(userID, targetID int64) {
		if relationshipType, ok := f.relationships[[2]int64{userID, targetID}]; ok {
			row := new(userv1.Relationship)
			row.SetUserId(userID)
			row.SetTargetId(targetID)
			row.SetType(userv1.RelationshipType(relationshipType))
			values = append(values, row)
		}
	}
	for _, targetID := range req.GetTargetIds() {
		appendRow(req.GetUserId(), targetID)
		if req.GetIncludeReverse() {
			appendRow(targetID, req.GetUserId())
		}
	}
	resp := new(userv1.CheckRelationshipsResponse)
	resp.SetRelationships(values)
	return resp, nil
}

func newDmTestServer(t *testing.T, fakeStore store.Store, publisher svc.EventPublisher, userClient userv1.UserServiceClient) messagev1.MessageServiceServer {
	t.Helper()
	node, err := snowflake.New()
	require.NoError(t, err)
	return New(&svc.ServiceContext{
		Cfg:         config.Config{Kafka: config.KafkaConfig{PublishTimeoutMs: 100}},
		Store:       fakeStore,
		Snowflake:   node,
		Publisher:   publisher,
		GuildClient: &fakeGuildClient{},
		UserClient:  userClient,
	})
}

func friendedUserClient(a, b int64) *fakeUserClient {
	client := newFakeUserClient()
	client.set(a, b, userv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND)
	client.set(b, a, userv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND)
	return client
}

func seedDmChannel(fake *fakeStore, channelID, userLo, userHi int64) *model.DmChannel {
	channel := &model.DmChannel{ID: channelID, UserLo: userLo, UserHi: userHi, CreatedAt: 1}
	fake.dmChannels[channelID] = channel
	return channel
}

func TestCreateDmChannelRequiresFriendship(t *testing.T) {
	fake := newFakeStore()
	publisher := new(fakePublisher)
	server := newDmTestServer(t, fake, publisher, newFakeUserClient())

	req := new(messagev1.CreateDmChannelRequest)
	req.SetUserId(1001)
	req.SetTargetId(2002)
	_, err := server.CreateDmChannel(context.Background(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.MessageDomain, rpcerror.MessageDmRequiresFriendship))
	require.Empty(t, publisher.records)
}

func TestCreateDmChannelIsIdempotentAndPublishesOnce(t *testing.T) {
	fake := newFakeStore()
	publisher := new(fakePublisher)
	server := newDmTestServer(t, fake, publisher, friendedUserClient(1001, 2002))

	req := new(messagev1.CreateDmChannelRequest)
	req.SetUserId(1001)
	req.SetTargetId(2002)
	resp, err := server.CreateDmChannel(context.Background(), req)
	require.NoError(t, err)
	channel := resp.GetChannel()
	require.Equal(t, int64(1001), channel.GetUserLo())
	require.Equal(t, int64(2002), channel.GetUserHi())

	// One user-routed record per participant.
	require.Len(t, publisher.records, 2)
	require.Equal(t, "1001", string(publisher.records[0].key))
	require.Equal(t, "2002", string(publisher.records[1].key))
	var envelope eventEnvelope[dmChannelCreatedPayload]
	require.NoError(t, json.Unmarshal(publisher.records[0].payload, &envelope))
	require.Equal(t, EventTypeDmChannelCreated, envelope.Type)
	require.Equal(t, "1001", envelope.Data.UserID)
	require.Equal(t, "2002", envelope.Data.RecipientID)

	// Reopening returns the same channel without new events.
	again, err := server.CreateDmChannel(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, channel.GetId(), again.GetChannel().GetId())
	require.Len(t, publisher.records, 2)

	// The reverse direction also lands on the same channel.
	req.SetUserId(2002)
	req.SetTargetId(1001)
	reverse, err := server.CreateDmChannel(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, channel.GetId(), reverse.GetChannel().GetId())
}

func TestCreateDmChannelValidation(t *testing.T) {
	server := newDmTestServer(t, newFakeStore(), nil, newFakeUserClient())

	req := new(messagev1.CreateDmChannelRequest)
	req.SetUserId(1001)
	req.SetTargetId(1001)
	_, err := server.CreateDmChannel(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestDmMessagesFlowBetweenParticipants(t *testing.T) {
	fake := newFakeStore()
	seedDmChannel(fake, 500, 1001, 2002)
	publisher := new(fakePublisher)
	server := newDmTestServer(t, fake, publisher, friendedUserClient(1001, 2002))

	createReq := new(messagev1.CreateMessageRequest)
	createReq.SetChannelId(500)
	createReq.SetAuthorId(1001)
	createReq.SetContent("hello dm")
	created, err := server.CreateMessage(context.Background(), createReq)
	require.NoError(t, err)
	messageID := created.GetMessage().GetId()
	require.Len(t, publisher.records, 1)
	require.Equal(t, "500", string(publisher.records[0].key))
	var envelope eventEnvelope[messagePayload]
	require.NoError(t, json.Unmarshal(publisher.records[0].payload, &envelope))
	require.Equal(t, EventTypeMessageCreated, envelope.Type)
	require.Empty(t, envelope.Data.UserID)
	require.Empty(t, envelope.Data.GuildID)

	// The other participant reads; outsiders see nothing.
	getReq := new(messagev1.GetMessageRequest)
	getReq.SetMessageId(messageID)
	getReq.SetUserId(2002)
	_, err = server.GetMessage(context.Background(), getReq)
	require.NoError(t, err)

	getReq.SetUserId(3003)
	_, err = server.GetMessage(context.Background(), getReq)
	require.Equal(t, codes.NotFound, status.Code(err))

	// DMs have no moderators: the peer cannot edit the author's message.
	updateReq := new(messagev1.UpdateMessageRequest)
	updateReq.SetMessageId(messageID)
	updateReq.SetActorUserId(2002)
	updateReq.SetContent("edited")
	_, err = server.UpdateMessage(context.Background(), updateReq)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestDmMessagesBlockedByEitherSide(t *testing.T) {
	fake := newFakeStore()
	seedDmChannel(fake, 500, 1001, 2002)
	userClient := friendedUserClient(1001, 2002)
	server := newDmTestServer(t, fake, nil, userClient)

	createReq := new(messagev1.CreateMessageRequest)
	createReq.SetChannelId(500)
	createReq.SetAuthorId(1001)
	createReq.SetContent("hello")

	// The sender blocked the recipient.
	userClient.set(1001, 2002, userv1.RelationshipType_RELATIONSHIP_TYPE_BLOCKED)
	_, err := server.CreateMessage(context.Background(), createReq)
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	// The recipient blocked the sender.
	userClient.set(1001, 2002, userv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND)
	userClient.set(2002, 1001, userv1.RelationshipType_RELATIONSHIP_TYPE_BLOCKED)
	_, err = server.CreateMessage(context.Background(), createReq)
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	// Reading history stays possible while blocked.
	listReq := new(messagev1.ListMessagesRequest)
	listReq.SetChannelId(500)
	listReq.SetUserId(1001)
	_, err = server.ListMessages(context.Background(), listReq)
	require.NoError(t, err)
}

func TestListDmChannelsPaginates(t *testing.T) {
	fake := newFakeStore()
	seedDmChannel(fake, 501, 1001, 2002)
	seedDmChannel(fake, 502, 1001, 2003)
	seedDmChannel(fake, 503, 2004, 9999)
	server := newDmTestServer(t, fake, nil, newFakeUserClient())

	req := new(messagev1.ListDmChannelsRequest)
	req.SetUserId(1001)
	resp, err := server.ListDmChannels(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetChannels(), 2)
	require.Equal(t, int64(502), resp.GetChannels()[0].GetId())
	require.Equal(t, int64(501), resp.GetBeforeId())

	req.SetBeforeId(502)
	req.SetLimit(1)
	resp, err = server.ListDmChannels(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetChannels(), 1)
	require.Equal(t, int64(501), resp.GetChannels()[0].GetId())
}

func TestAuthorizeDmChannel(t *testing.T) {
	fake := newFakeStore()
	seedDmChannel(fake, 500, 1001, 2002)
	server := newDmTestServer(t, fake, nil, newFakeUserClient())

	req := new(messagev1.AuthorizeDmChannelRequest)
	req.SetChannelId(500)
	req.SetUserId(1001)
	resp, err := server.AuthorizeDmChannel(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.GetAllowed())

	req.SetUserId(3003)
	resp, err = server.AuthorizeDmChannel(context.Background(), req)
	require.NoError(t, err)
	require.False(t, resp.GetAllowed())

	req.SetChannelId(999)
	_, err = server.AuthorizeDmChannel(context.Background(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
}
