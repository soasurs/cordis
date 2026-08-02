package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
)

func TestCreateGuildChannelUsesAuthenticatedActor(t *testing.T) {
	channel := new(guildv1.GuildChannel)
	channel.SetId(5001)
	channel.SetGuildId(3001)
	channel.SetName("general")
	channel.SetType(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT)
	resp := new(guildv1.CreateGuildChannelResponse)
	resp.SetChannel(channel)
	guildClient := &fakeGuildClient{createChannelResponse: resp}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	createChannelReq := new(apiv1.CreateGuildChannelRequest)
	createChannelReq.SetGuildId(3001)
	createChannelReq.SetName("general")
	createChannelReq.SetType(apiv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT)
	createChannelReq.SetExpectedChannelLayoutRevision(1)
	result, err := client.CreateGuildChannel(context.Background(), createChannelReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.createChannelRequest.GetActorUserId())
	require.Equal(t, int64(5001), result.GetChannel().GetId())
}

func TestGetGuildChannelMapsRequestAndResponse(t *testing.T) {
	guildClient := &fakeGuildClient{
		getChannelFn: func(*guildv1.GetGuildChannelRequest) (*guildv1.GetGuildChannelResponse, error) {
			resp := new(guildv1.GetGuildChannelResponse)
			resp.SetChannel(internalGuildChannel())
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	getChannelReq := new(apiv1.GetGuildChannelRequest)
	getChannelReq.SetChannelId(5001)
	resp, err := client.GetGuildChannel(context.Background(), getChannelReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.getChannelReq.GetActorUserId())
	require.Equal(t, int64(5001), resp.GetChannel().GetId())
}

func TestListGuildChannelsMapsResponse(t *testing.T) {
	guildClient := &fakeGuildClient{
		listChannelsFn: func(*guildv1.ListGuildChannelsRequest) (*guildv1.ListGuildChannelsResponse, error) {
			resp := new(guildv1.ListGuildChannelsResponse)
			resp.SetChannels([]*guildv1.GuildChannel{internalGuildChannel()})
			resp.SetChannelLayoutRevision(2)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	listChannelsReq := new(apiv1.ListGuildChannelsRequest)
	listChannelsReq.SetGuildId(3001)
	resp, err := client.ListGuildChannels(context.Background(), listChannelsReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.listChannelsReq.GetActorUserId())
	require.Len(t, resp.GetChannels(), 1)
	require.Equal(t, int64(2), resp.GetChannelLayoutRevision())
}

func TestUpdateGuildChannelPreservesFieldPresence(t *testing.T) {
	guildClient := &fakeGuildClient{
		updateChannelFn: func(*guildv1.UpdateGuildChannelRequest) (*guildv1.UpdateGuildChannelResponse, error) {
			resp := new(guildv1.UpdateGuildChannelResponse)
			resp.SetChannel(internalGuildChannel())
			resp.SetChannelLayoutRevision(2)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	updateChannelReq := new(apiv1.UpdateGuildChannelRequest)
	updateChannelReq.SetChannelId(5001)
	updateChannelReq.SetName("renamed")
	updateChannelReq.SetParentId(0)
	updateChannelReq.SetExpectedChannelLayoutRevision(1)
	resp, err := client.UpdateGuildChannel(context.Background(), updateChannelReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.updateChannelReq.GetActorUserId())
	require.True(t, guildClient.updateChannelReq.HasName())
	require.False(t, guildClient.updateChannelReq.HasTopic())
	require.True(t, guildClient.updateChannelReq.HasParentId())
	require.Equal(t, int64(1), guildClient.updateChannelReq.GetExpectedChannelLayoutRevision())
	require.True(t, resp.HasChannelLayoutRevision())
	require.Equal(t, int64(2), resp.GetChannelLayoutRevision())
}

func TestUpdateGuildChannelMetadataDoesNotForwardLayoutRevision(t *testing.T) {
	guildClient := &fakeGuildClient{
		updateChannelFn: func(*guildv1.UpdateGuildChannelRequest) (*guildv1.UpdateGuildChannelResponse, error) {
			resp := new(guildv1.UpdateGuildChannelResponse)
			resp.SetChannel(internalGuildChannel())
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	updateChannelReq := new(apiv1.UpdateGuildChannelRequest)
	updateChannelReq.SetChannelId(5001)
	updateChannelReq.SetName("renamed")
	resp, err := client.UpdateGuildChannel(context.Background(), updateChannelReq)
	require.NoError(t, err)
	require.False(t, guildClient.updateChannelReq.HasExpectedChannelLayoutRevision())
	require.False(t, resp.HasChannelLayoutRevision())
}

func TestDeleteGuildChannelMapsRequest(t *testing.T) {
	guildClient := &fakeGuildClient{
		deleteChannelFn: func(*guildv1.DeleteGuildChannelRequest) (*guildv1.DeleteGuildChannelResponse, error) {
			resp := new(guildv1.DeleteGuildChannelResponse)
			resp.SetOk(true)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	deleteChannelReq := new(apiv1.DeleteGuildChannelRequest)
	deleteChannelReq.SetChannelId(5001)
	deleteChannelReq.SetExpectedChannelLayoutRevision(1)
	resp, err := client.DeleteGuildChannel(context.Background(), deleteChannelReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.deleteChannelReq.GetActorUserId())
	require.Equal(t, int64(1), guildClient.deleteChannelReq.GetExpectedChannelLayoutRevision())
	require.True(t, resp.GetOk())
}

func TestReorderGuildChannelsMapsPositions(t *testing.T) {
	guildClient := &fakeGuildClient{
		reorderChannelsFn: func(*guildv1.ReorderGuildChannelsRequest) (*guildv1.ReorderGuildChannelsResponse, error) {
			resp := new(guildv1.ReorderGuildChannelsResponse)
			resp.SetChannels([]*guildv1.GuildChannel{internalGuildChannel()})
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	pos := new(apiv1.GuildChannelPosition)
	pos.SetChannelId(5001)
	pos.SetPosition(0)
	pos.SetParentId(0)
	reorderChannelsReq := new(apiv1.ReorderGuildChannelsRequest)
	reorderChannelsReq.SetGuildId(3001)
	reorderChannelsReq.SetExpectedChannelLayoutRevision(1)
	reorderChannelsReq.SetPositions([]*apiv1.GuildChannelPosition{pos})
	resp, err := client.ReorderGuildChannels(context.Background(), reorderChannelsReq)
	require.NoError(t, err)
	require.Len(t, resp.GetChannels(), 1)
	require.Equal(t, int64(1001), guildClient.reorderChannelsReq.GetActorUserId())
	require.Equal(t, int64(1), guildClient.reorderChannelsReq.GetExpectedChannelLayoutRevision())
	require.Len(t, guildClient.reorderChannelsReq.GetPositions(), 1)
	require.Equal(t, int64(5001), guildClient.reorderChannelsReq.GetPositions()[0].GetChannelId())
	require.True(t, guildClient.reorderChannelsReq.GetPositions()[0].HasParentId())
	require.Zero(t, guildClient.reorderChannelsReq.GetPositions()[0].GetParentId())
}

func TestUpsertGuildChannelPermissionOverwriteMapsRequestAndResponse(t *testing.T) {
	overwrite := new(guildv1.GuildChannelPermissionOverwrite)
	overwrite.SetChannelId(5001)
	overwrite.SetGuildId(3001)
	overwrite.SetAppliesTo(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE)
	overwrite.SetAppliesToId(4001)
	overwrite.SetAllow(8)
	overwrite.SetDeny(4)
	guildClient := &fakeGuildClient{
		upsertOverwriteFn: func(*guildv1.UpsertGuildChannelPermissionOverwriteRequest) (*guildv1.UpsertGuildChannelPermissionOverwriteResponse, error) {
			resp := new(guildv1.UpsertGuildChannelPermissionOverwriteResponse)
			resp.SetOverwrite(overwrite)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	upsertOverwriteReq := new(apiv1.UpsertGuildChannelPermissionOverwriteRequest)
	upsertOverwriteReq.SetChannelId(5001)
	upsertOverwriteReq.SetAppliesTo(apiv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE)
	upsertOverwriteReq.SetAppliesToId(4001)
	upsertOverwriteReq.SetAllow(8)
	upsertOverwriteReq.SetDeny(4)
	resp, err := client.UpsertGuildChannelPermissionOverwrite(context.Background(), upsertOverwriteReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.upsertOverwriteReq.GetActorUserId())
	require.Equal(t, int64(5001), resp.GetOverwrite().GetChannelId())
	require.Equal(t, uint64(8), resp.GetOverwrite().GetAllow())
}

func TestDeleteGuildChannelPermissionOverwriteMapsRequest(t *testing.T) {
	guildClient := &fakeGuildClient{
		deleteOverwriteFn: func(*guildv1.DeleteGuildChannelPermissionOverwriteRequest) (*guildv1.DeleteGuildChannelPermissionOverwriteResponse, error) {
			resp := new(guildv1.DeleteGuildChannelPermissionOverwriteResponse)
			resp.SetOk(true)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	deleteOverwriteReq := new(apiv1.DeleteGuildChannelPermissionOverwriteRequest)
	deleteOverwriteReq.SetChannelId(5001)
	deleteOverwriteReq.SetAppliesTo(apiv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER)
	deleteOverwriteReq.SetAppliesToId(1002)
	resp, err := client.DeleteGuildChannelPermissionOverwrite(context.Background(), deleteOverwriteReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.deleteOverwriteReq.GetActorUserId())
	require.True(t, resp.GetOk())
}

func TestListGuildChannelPermissionOverwritesMapsResponse(t *testing.T) {
	overwrite := new(guildv1.GuildChannelPermissionOverwrite)
	overwrite.SetChannelId(5001)
	overwrite.SetGuildId(3001)
	overwrite.SetAppliesToId(4001)
	guildClient := &fakeGuildClient{
		listOverwritesFn: func(*guildv1.ListGuildChannelPermissionOverwritesRequest) (*guildv1.ListGuildChannelPermissionOverwritesResponse, error) {
			resp := new(guildv1.ListGuildChannelPermissionOverwritesResponse)
			resp.SetOverwrites([]*guildv1.GuildChannelPermissionOverwrite{overwrite})
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	listOverwritesReq := new(apiv1.ListGuildChannelPermissionOverwritesRequest)
	listOverwritesReq.SetChannelId(5001)
	resp, err := client.ListGuildChannelPermissionOverwrites(context.Background(), listOverwritesReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.listOverwritesReq.GetActorUserId())
	require.Len(t, resp.GetOverwrites(), 1)
}
