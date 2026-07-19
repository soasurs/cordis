package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

func TestChannelPermissionsApplyOverwritePrecedence(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.roles[10][20] = testRole(20, 10, "member", PermissionSendMessages, 1)
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1002, 20, 1))
	authority, err := loadMemberAuthority(t.Context(), fakeStore, 10, 1002)
	require.NoError(t, err)
	roles, err := fakeStore.ListGuildMemberRoles(t.Context(), 10, 1002)
	require.NoError(t, err)

	overwrites := []*model.ChannelPermissionOverwrite{
		{TargetType: int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE), TargetID: 10, Deny: PermissionViewChannel},
		{TargetType: int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE), TargetID: 20, Allow: PermissionViewChannel},
		{TargetType: int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER), TargetID: 1002, Deny: PermissionSendMessages},
	}
	permissions := channelPermissions(authority, roles, overwrites, 1002)
	require.NotZero(t, permissions&PermissionViewChannel)
	require.Zero(t, permissions&PermissionSendMessages)
}

func TestChannelPermissionsRemoveSendWhenViewDenied(t *testing.T) {
	fakeStore := roleTestStore()
	authority, err := loadMemberAuthority(t.Context(), fakeStore, 10, 1002)
	require.NoError(t, err)
	roles, err := fakeStore.ListGuildMemberRoles(t.Context(), 10, 1002)
	require.NoError(t, err)
	permissions := channelPermissions(authority, roles, []*model.ChannelPermissionOverwrite{{
		TargetType: int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER),
		TargetID:   1002, Deny: PermissionViewChannel,
	}}, 1002)
	require.Zero(t, permissions&PermissionViewChannel)
	require.Zero(t, permissions&PermissionSendMessages)
}

func TestCreateAndAuthorizeGuildChannel(t *testing.T) {
	fakeStore := roleTestStore()
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)

	create := new(guildv1.CreateGuildChannelRequest)
	create.SetGuildId(10)
	create.SetActorUserId(1001)
	create.SetName(" general ")
	resp, err := server.CreateGuildChannel(t.Context(), create)
	require.NoError(t, err)
	require.Equal(t, "general", resp.GetChannel().GetName())
	require.Equal(t, guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT, resp.GetChannel().GetType())

	var envelope eventEnvelope[guildChannelPayload]
	require.NoError(t, json.Unmarshal(publisher.onlyRecord(t).payload, &envelope))
	require.Equal(t, EventTypeGuildChannelCreated, envelope.Type)
	require.Equal(t, "10", envelope.Data.GuildID)

	authorize := new(guildv1.AuthorizeGuildChannelRequest)
	authorize.SetChannelId(resp.GetChannel().GetId())
	authorize.SetUserId(1002)
	authorize.SetPermission(PermissionViewChannel | PermissionSendMessages)
	authResp, err := server.AuthorizeGuildChannel(t.Context(), authorize)
	require.NoError(t, err)
	require.True(t, authResp.GetAllowed())
}

func TestAuthorizeGuildChannelsBatchesPermissionInputs(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.channels[101] = &model.Channel{ID: 101, GuildID: 10, Type: int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT)}
	fakeStore.channels[102] = &model.Channel{ID: 102, GuildID: 10, Type: int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_VOICE)}
	server := newTestGuildServer(t, fakeStore, nil)
	req := new(guildv1.AuthorizeGuildChannelsRequest)
	req.SetUserId(1002)
	req.SetChannelIds([]int64{102, 101})
	req.SetPermission(PermissionViewChannel)

	resp, err := server.AuthorizeGuildChannels(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetAuthorizations(), 2)
	require.Equal(t, int64(102), resp.GetAuthorizations()[0].GetChannelId())
	require.True(t, resp.GetAuthorizations()[0].GetAllowed())
	require.Equal(t, guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_VOICE, resp.GetAuthorizations()[0].GetChannelType())
	require.Equal(t, int64(101), resp.GetAuthorizations()[1].GetChannelId())
}

func TestCategoryAndVoiceChannelMetadata(t *testing.T) {
	fakeStore := roleTestStore()
	server := newTestGuildServer(t, fakeStore, nil)

	categoryReq := new(guildv1.CreateGuildChannelRequest)
	categoryReq.SetGuildId(10)
	categoryReq.SetActorUserId(1001)
	categoryReq.SetName("rooms")
	categoryReq.SetType(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_CATEGORY)
	categoryResp, err := server.CreateGuildChannel(t.Context(), categoryReq)
	require.NoError(t, err)

	voiceReq := new(guildv1.CreateGuildChannelRequest)
	voiceReq.SetGuildId(10)
	voiceReq.SetActorUserId(1001)
	voiceReq.SetName("lounge")
	voiceReq.SetType(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_VOICE)
	voiceReq.SetParentId(categoryResp.GetChannel().GetId())
	voiceResp, err := server.CreateGuildChannel(t.Context(), voiceReq)
	require.NoError(t, err)
	require.Equal(t, categoryResp.GetChannel().GetId(), voiceResp.GetChannel().GetParentId())

	deleteReq := new(guildv1.DeleteGuildChannelRequest)
	deleteReq.SetChannelId(categoryResp.GetChannel().GetId())
	deleteReq.SetActorUserId(1001)
	_, err = server.DeleteGuildChannel(t.Context(), deleteReq)
	require.NoError(t, err)
	require.Zero(t, fakeStore.channels[voiceResp.GetChannel().GetId()].ParentID)
}

func TestChannelOverwriteCanHideChannel(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.channels[30] = &model.Channel{
		ID: 30, GuildID: 10, Name: "private", Type: int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT), Revision: 1,
	}
	server := newTestGuildServer(t, fakeStore, new(fakePublisher))

	upsert := new(guildv1.UpsertGuildChannelPermissionOverwriteRequest)
	upsert.SetChannelId(30)
	upsert.SetActorUserId(1001)
	upsert.SetTargetType(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER)
	upsert.SetTargetId(1002)
	upsert.SetDeny(PermissionViewChannel)
	_, err := server.UpsertGuildChannelPermissionOverwrite(t.Context(), upsert)
	require.NoError(t, err)

	get := new(guildv1.GetGuildChannelRequest)
	get.SetChannelId(30)
	get.SetActorUserId(1002)
	_, err = server.GetGuildChannel(t.Context(), get)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestListGuildChannelsLoadsOverwritesOnce(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.channels[30] = &model.Channel{
		ID: 30, GuildID: 10, Name: "private", Type: int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT), Revision: 1,
	}
	fakeStore.channels[31] = &model.Channel{
		ID: 31, GuildID: 10, Name: "general", Type: int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT), Revision: 1,
	}
	fakeStore.overwrites[30] = map[string]*model.ChannelPermissionOverwrite{
		overwriteKey(int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER), 1002): {
			ChannelID:  30,
			GuildID:    10,
			TargetType: int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER),
			TargetID:   1002,
			Deny:       PermissionViewChannel,
		},
	}
	server := newTestGuildServer(t, fakeStore, nil)

	req := new(guildv1.ListGuildChannelsRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1002)
	resp, err := server.ListGuildChannels(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetChannels(), 1)
	require.Equal(t, int64(31), resp.GetChannels()[0].GetId())
	require.Equal(t, 1, fakeStore.listOverwritesByGuildCalls)
	require.Zero(t, fakeStore.listOverwritesByChannelCalls)
}

func TestChannelOverwriteRejectsGuildOnlyPermission(t *testing.T) {
	req := new(guildv1.UpsertGuildChannelPermissionOverwriteRequest)
	req.SetChannelId(30)
	req.SetActorUserId(1001)
	req.SetTargetType(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER)
	req.SetTargetId(1002)
	req.SetAllow(PermissionManageGuild)
	server := newTestGuildServer(t, roleTestStore(), nil)
	_, err := server.UpsertGuildChannelPermissionOverwrite(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
