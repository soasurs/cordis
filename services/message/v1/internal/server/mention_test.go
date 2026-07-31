package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

func guildRole(id int64) *guildv1.GuildRole {
	role := new(guildv1.GuildRole)
	role.SetId(id)
	role.SetGuildId(9001)
	role.SetName("role-" + string(rune('0'+id%10)))
	role.SetRevision(1)
	return role
}

func createMentionRequest(content string) *messagev1.CreateMessageRequest {
	req := new(messagev1.CreateMessageRequest)
	req.SetChannelId(10)
	req.SetAuthorId(20)
	req.SetContent(content)
	return req
}

func TestCreateMessageParsesUserRoleAndEveryoneMentions(t *testing.T) {
	fakeStore := newFakeStore()
	guildClient := &fakeGuildClient{
		roles:       []*guildv1.GuildRole{guildRole(40)},
		permissions: permissionViewChannel | permissionSendMessages | permissionMentionEveryone,
	}
	publisher := new(fakePublisher)
	server := newTestMessageServerWithGuild(t, fakeStore, publisher, guildClient)

	resp, err := server.CreateMessage(t.Context(), createMentionRequest("hi <@30> <@&40> @everyone"))
	require.NoError(t, err)
	require.Equal(t, []int64{30}, resp.GetMessage().GetMentionUserIds())
	require.Equal(t, []int64{40}, resp.GetMessage().GetMentionRoleIds())
	require.True(t, resp.GetMessage().GetMentionEveryone())
	mentions := fakeStore.mentions[resp.GetMessage().GetId()]
	require.Equal(t, []int64{30}, mentions.UserIDs)
	require.Equal(t, []int64{40}, mentions.RoleIDs)
	require.True(t, mentions.Everyone)

	var envelope eventEnvelope[messagePayload]
	require.NoError(t, json.Unmarshal(publisher.records[0].payload, &envelope))
	require.Equal(t, []string{"30"}, envelope.Data.MentionUserIDs)
	require.Equal(t, []string{"40"}, envelope.Data.MentionRoleIDs)
	require.True(t, envelope.Data.MentionEveryone)
}

func TestCreateMessageResponseMentionsSortedAscending(t *testing.T) {
	fakeStore := newFakeStore()
	publisher := new(fakePublisher)
	guildClient := &fakeGuildClient{
		roles:       []*guildv1.GuildRole{guildRole(50), guildRole(40)},
		permissions: permissionViewChannel | permissionSendMessages | permissionMentionEveryone,
	}
	server := newTestMessageServerWithGuild(t, fakeStore, publisher, guildClient)

	resp, err := server.CreateMessage(t.Context(), createMentionRequest("<@200> <@100> <@&50> <@&40> @everyone"))
	require.NoError(t, err)
	require.Equal(t, []int64{100, 200}, resp.GetMessage().GetMentionUserIds())
	require.Equal(t, []int64{40, 50}, resp.GetMessage().GetMentionRoleIds())

	var envelope eventEnvelope[messagePayload]
	require.NoError(t, json.Unmarshal(publisher.records[0].payload, &envelope))
	require.Equal(t, []string{"100", "200"}, envelope.Data.MentionUserIDs)
	require.Equal(t, []string{"40", "50"}, envelope.Data.MentionRoleIDs)
}

func TestCreateMessageEveryoneRequiresPermission(t *testing.T) {
	fakeStore := newFakeStore()
	publisher := new(fakePublisher)
	server := newTestMessageServerWithGuild(t, fakeStore, publisher, &fakeGuildClient{})

	_, err := server.CreateMessage(t.Context(), createMentionRequest("@everyone hello"))
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	secondStore := newFakeStore()
	guildClient := &fakeGuildClient{permissions: permissionViewChannel | permissionSendMessages | permissionMentionEveryone}
	server = newTestMessageServerWithGuild(t, secondStore, new(fakePublisher), guildClient)
	resp, err := server.CreateMessage(t.Context(), createMentionRequest("@everyone hello"))
	require.NoError(t, err)
	require.True(t, secondStore.mentions[resp.GetMessage().GetId()].Everyone)
}

func TestCreateMessageDmKeepsOnlyUserMentions(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.dmChannels[10] = &model.DmChannel{ID: 10, UserLo: 20, UserHi: 21}
	server := newTestMessageServer(t, fakeStore, new(fakePublisher))

	resp, err := server.CreateMessage(t.Context(), createMentionRequest("hi <@21> <@&40> @everyone"))
	require.NoError(t, err)
	mentions := fakeStore.mentions[resp.GetMessage().GetId()]
	require.Equal(t, []int64{21}, mentions.UserIDs)
	require.Empty(t, mentions.RoleIDs)
	require.False(t, mentions.Everyone)
}

func TestCreateMessageDmFiltersUnknownUsers(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.dmChannels[10] = &model.DmChannel{ID: 10, UserLo: 20, UserHi: 21}
	userClient := newFakeUserClient()
	userClient.missingUsers = map[int64]bool{30: true}
	server := newTestMessageServerWithClients(t, fakeStore, new(fakePublisher), &fakeGuildClient{}, userClient)

	resp, err := server.CreateMessage(t.Context(), createMentionRequest("hi <@30> <@31>"))
	require.NoError(t, err)
	mentions := fakeStore.mentions[resp.GetMessage().GetId()]
	require.Equal(t, []int64{31}, mentions.UserIDs)
}

func TestCreateMessageFiltersUnknownRolesAndUsers(t *testing.T) {
	fakeStore := newFakeStore()
	guildClient := &fakeGuildClient{roles: []*guildv1.GuildRole{guildRole(40)}}
	userClient := newFakeUserClient()
	userClient.missingUsers = map[int64]bool{30: true}
	server := newTestMessageServerWithClients(t, fakeStore, new(fakePublisher), guildClient, userClient)

	resp, err := server.CreateMessage(t.Context(), createMentionRequest("hi <@30> <@31> <@&40> <@&41>"))
	require.NoError(t, err)
	mentions := fakeStore.mentions[resp.GetMessage().GetId()]
	require.Equal(t, []int64{31}, mentions.UserIDs)
	require.Equal(t, []int64{40}, mentions.RoleIDs)
}

func TestUpdateMessageRebuildsMentionsFromContent(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.messages[100] = &model.Message{
		ID: 100, ChannelID: 10, AuthorID: 20, Content: "old <@40>",
		Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT), Revision: 1,
	}
	fakeStore.mentions[100] = model.MessageMentions{UserIDs: []int64{40}}
	guildClient := &fakeGuildClient{roles: []*guildv1.GuildRole{guildRole(50)}}
	publisher := new(fakePublisher)
	server := newTestMessageServerWithGuild(t, fakeStore, publisher, guildClient)

	req := new(messagev1.UpdateMessageRequest)
	req.SetMessageId(100)
	req.SetActorUserId(20)
	req.SetContent("edited <@30> <@&50>")
	resp, err := server.UpdateMessage(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []int64{30}, resp.GetMessage().GetMentionUserIds())
	require.Equal(t, []int64{50}, resp.GetMessage().GetMentionRoleIds())

	mentions := fakeStore.mentions[100]
	require.Equal(t, []int64{30}, mentions.UserIDs)
	require.Equal(t, []int64{50}, mentions.RoleIDs)

	var envelope eventEnvelope[messagePayload]
	require.NoError(t, json.Unmarshal(publisher.onlyRecord(t).payload, &envelope))
	require.Equal(t, EventTypeMessageUpdated, envelope.Type)
	require.Equal(t, []string{"30"}, envelope.Data.MentionUserIDs)
	require.Equal(t, []string{"50"}, envelope.Data.MentionRoleIDs)
	require.Equal(t, []string{"40"}, envelope.Data.PreviousMentionUserIDs)
	require.Equal(t, int64(2), resp.GetMessage().GetRevision())
}

func TestUpdateMessageResponseClearsEveryone(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.messages[100] = &model.Message{
		ID: 100, ChannelID: 10, AuthorID: 20, Content: "@everyone old",
		Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT), Revision: 1,
	}
	fakeStore.mentions[100] = model.MessageMentions{Everyone: true}
	server := newTestMessageServer(t, fakeStore, new(fakePublisher))

	req := new(messagev1.UpdateMessageRequest)
	req.SetMessageId(100)
	req.SetActorUserId(20)
	req.SetContent("hi <@30>")
	resp, err := server.UpdateMessage(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []int64{30}, resp.GetMessage().GetMentionUserIds())
	require.False(t, resp.GetMessage().GetMentionEveryone())
}

func TestGetAndListMessagesIncludeMentions(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.messages[100] = &model.Message{
		ID: 100, ChannelID: 10, AuthorID: 20, Content: "hello",
		Type: int32(messagev1.MessageType_MESSAGE_TYPE_DEFAULT), Revision: 1,
		Mentions: model.MessageMentions{Everyone: true},
	}
	fakeStore.mentions[100] = model.MessageMentions{UserIDs: []int64{30}, RoleIDs: []int64{40}, Everyone: true}
	server := newTestMessageServer(t, fakeStore, new(fakePublisher))

	getReq := new(messagev1.GetMessageRequest)
	getReq.SetMessageId(100)
	getReq.SetUserId(20)
	getResp, err := server.GetMessage(t.Context(), getReq)
	require.NoError(t, err)
	require.Equal(t, []int64{30}, getResp.GetMessage().GetMentionUserIds())
	require.Equal(t, []int64{40}, getResp.GetMessage().GetMentionRoleIds())
	require.True(t, getResp.GetMessage().GetMentionEveryone())

	listReq := new(messagev1.ListMessagesRequest)
	listReq.SetChannelId(10)
	listReq.SetUserId(20)
	listResp, err := server.ListMessages(t.Context(), listReq)
	require.NoError(t, err)
	require.Len(t, listResp.GetMessages(), 1)
	require.Equal(t, []int64{30}, listResp.GetMessages()[0].GetMentionUserIds())
	require.Equal(t, []int64{40}, listResp.GetMessages()[0].GetMentionRoleIds())
	require.True(t, listResp.GetMessages()[0].GetMentionEveryone())
}
