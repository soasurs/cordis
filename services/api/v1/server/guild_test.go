package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

type fakeGuildClient struct {
	guildv1.GuildServiceClient
	createRequest         *guildv1.CreateGuildRequest
	updateRequest         *guildv1.UpdateGuildRequest
	addMemberRequest      *guildv1.AddGuildMemberRequest
	updateMemberRequest   *guildv1.UpdateGuildMemberRequest
	leaveRequest          *guildv1.LeaveGuildRequest
	transferRequest       *guildv1.TransferGuildOwnershipRequest
	createRoleRequest     *guildv1.CreateGuildRoleRequest
	createChannelRequest  *guildv1.CreateGuildChannelRequest
	createResponse        *guildv1.CreateGuildResponse
	updateResponse        *guildv1.UpdateGuildResponse
	addMemberResponse     *guildv1.AddGuildMemberResponse
	updateMemberResponse  *guildv1.UpdateGuildMemberResponse
	leaveResponse         *guildv1.LeaveGuildResponse
	transferResponse      *guildv1.TransferGuildOwnershipResponse
	createRoleResponse    *guildv1.CreateGuildRoleResponse
	createChannelResponse *guildv1.CreateGuildChannelResponse

	getGuildReq   *guildv1.GetGuildRequest
	getGuildFn    func(*guildv1.GetGuildRequest) (*guildv1.GetGuildResponse, error)
	listGuildsReq *guildv1.ListUserGuildsRequest
	listGuildsFn  func(*guildv1.ListUserGuildsRequest) (*guildv1.ListUserGuildsResponse, error)
	deleteReq     *guildv1.DeleteGuildRequest
	deleteFn      func(*guildv1.DeleteGuildRequest) (*guildv1.DeleteGuildResponse, error)

	getMemberReq   *guildv1.GetGuildMemberRequest
	getMemberFn    func(*guildv1.GetGuildMemberRequest) (*guildv1.GetGuildMemberResponse, error)
	listMembersReq *guildv1.ListGuildMembersRequest
	listMembersFn  func(*guildv1.ListGuildMembersRequest) (*guildv1.ListGuildMembersResponse, error)
	kickReq        *guildv1.KickGuildMemberRequest
	kickFn         func(*guildv1.KickGuildMemberRequest) (*guildv1.KickGuildMemberResponse, error)
	banReq         *guildv1.BanGuildMemberRequest
	banFn          func(*guildv1.BanGuildMemberRequest) (*guildv1.BanGuildMemberResponse, error)
	unbanReq       *guildv1.UnbanGuildMemberRequest
	unbanFn        func(*guildv1.UnbanGuildMemberRequest) (*guildv1.UnbanGuildMemberResponse, error)
	listBansReq    *guildv1.ListGuildBansRequest
	listBansFn     func(*guildv1.ListGuildBansRequest) (*guildv1.ListGuildBansResponse, error)

	getRoleReq          *guildv1.GetGuildRoleRequest
	getRoleFn           func(*guildv1.GetGuildRoleRequest) (*guildv1.GetGuildRoleResponse, error)
	listRolesReq        *guildv1.ListGuildRolesRequest
	listRolesFn         func(*guildv1.ListGuildRolesRequest) (*guildv1.ListGuildRolesResponse, error)
	updateRoleReq       *guildv1.UpdateGuildRoleRequest
	updateRoleFn        func(*guildv1.UpdateGuildRoleRequest) (*guildv1.UpdateGuildRoleResponse, error)
	deleteRoleReq       *guildv1.DeleteGuildRoleRequest
	deleteRoleFn        func(*guildv1.DeleteGuildRoleRequest) (*guildv1.DeleteGuildRoleResponse, error)
	reorderRolesReq     *guildv1.ReorderGuildRolesRequest
	reorderRolesFn      func(*guildv1.ReorderGuildRolesRequest) (*guildv1.ReorderGuildRolesResponse, error)
	addMemberRoleReq    *guildv1.AddGuildMemberRoleRequest
	addMemberRoleFn     func(*guildv1.AddGuildMemberRoleRequest) (*guildv1.AddGuildMemberRoleResponse, error)
	removeMemberRoleReq *guildv1.RemoveGuildMemberRoleRequest
	removeMemberRoleFn  func(*guildv1.RemoveGuildMemberRoleRequest) (*guildv1.RemoveGuildMemberRoleResponse, error)
	listMemberRolesReq  *guildv1.ListGuildMemberRolesRequest
	listMemberRolesFn   func(*guildv1.ListGuildMemberRolesRequest) (*guildv1.ListGuildMemberRolesResponse, error)
	permissionsReq      *guildv1.GetGuildMemberPermissionsRequest
	permissionsFn       func(*guildv1.GetGuildMemberPermissionsRequest) (*guildv1.GetGuildMemberPermissionsResponse, error)

	getChannelReq      *guildv1.GetGuildChannelRequest
	getChannelFn       func(*guildv1.GetGuildChannelRequest) (*guildv1.GetGuildChannelResponse, error)
	listChannelsReq    *guildv1.ListGuildChannelsRequest
	listChannelsFn     func(*guildv1.ListGuildChannelsRequest) (*guildv1.ListGuildChannelsResponse, error)
	updateChannelReq   *guildv1.UpdateGuildChannelRequest
	updateChannelFn    func(*guildv1.UpdateGuildChannelRequest) (*guildv1.UpdateGuildChannelResponse, error)
	deleteChannelReq   *guildv1.DeleteGuildChannelRequest
	deleteChannelFn    func(*guildv1.DeleteGuildChannelRequest) (*guildv1.DeleteGuildChannelResponse, error)
	reorderChannelsReq *guildv1.ReorderGuildChannelsRequest
	reorderChannelsFn  func(*guildv1.ReorderGuildChannelsRequest) (*guildv1.ReorderGuildChannelsResponse, error)
	upsertOverwriteReq *guildv1.UpsertGuildChannelPermissionOverwriteRequest
	upsertOverwriteFn  func(*guildv1.UpsertGuildChannelPermissionOverwriteRequest) (*guildv1.UpsertGuildChannelPermissionOverwriteResponse, error)
	deleteOverwriteReq *guildv1.DeleteGuildChannelPermissionOverwriteRequest
	deleteOverwriteFn  func(*guildv1.DeleteGuildChannelPermissionOverwriteRequest) (*guildv1.DeleteGuildChannelPermissionOverwriteResponse, error)
	listOverwritesReq  *guildv1.ListGuildChannelPermissionOverwritesRequest
	listOverwritesFn   func(*guildv1.ListGuildChannelPermissionOverwritesRequest) (*guildv1.ListGuildChannelPermissionOverwritesResponse, error)

	createInviteReq *guildv1.CreateGuildInviteRequest
	createInviteFn  func(*guildv1.CreateGuildInviteRequest) (*guildv1.CreateGuildInviteResponse, error)
	getInviteReq    *guildv1.GetGuildInviteRequest
	getInviteFn     func(*guildv1.GetGuildInviteRequest) (*guildv1.GetGuildInviteResponse, error)
	listInvitesReq  *guildv1.ListGuildInvitesRequest
	listInvitesFn   func(*guildv1.ListGuildInvitesRequest) (*guildv1.ListGuildInvitesResponse, error)
	deleteInviteReq *guildv1.DeleteGuildInviteRequest
	deleteInviteFn  func(*guildv1.DeleteGuildInviteRequest) (*guildv1.DeleteGuildInviteResponse, error)
	joinInviteReq   *guildv1.JoinGuildByInviteRequest
	joinInviteFn    func(*guildv1.JoinGuildByInviteRequest) (*guildv1.JoinGuildByInviteResponse, error)
}

func (f *fakeGuildClient) CreateGuildRole(_ context.Context, req *guildv1.CreateGuildRoleRequest, _ ...grpc.CallOption) (*guildv1.CreateGuildRoleResponse, error) {
	f.createRoleRequest = req
	return f.createRoleResponse, nil
}

func (f *fakeGuildClient) CreateGuildChannel(_ context.Context, req *guildv1.CreateGuildChannelRequest, _ ...grpc.CallOption) (*guildv1.CreateGuildChannelResponse, error) {
	f.createChannelRequest = req
	return f.createChannelResponse, nil
}

func (f *fakeGuildClient) AddGuildMember(_ context.Context, req *guildv1.AddGuildMemberRequest, _ ...grpc.CallOption) (*guildv1.AddGuildMemberResponse, error) {
	f.addMemberRequest = req
	return f.addMemberResponse, nil
}

func (f *fakeGuildClient) UpdateGuildMember(_ context.Context, req *guildv1.UpdateGuildMemberRequest, _ ...grpc.CallOption) (*guildv1.UpdateGuildMemberResponse, error) {
	f.updateMemberRequest = req
	return f.updateMemberResponse, nil
}

func (f *fakeGuildClient) LeaveGuild(_ context.Context, req *guildv1.LeaveGuildRequest, _ ...grpc.CallOption) (*guildv1.LeaveGuildResponse, error) {
	f.leaveRequest = req
	return f.leaveResponse, nil
}

func (f *fakeGuildClient) TransferGuildOwnership(_ context.Context, req *guildv1.TransferGuildOwnershipRequest, _ ...grpc.CallOption) (*guildv1.TransferGuildOwnershipResponse, error) {
	f.transferRequest = req
	return f.transferResponse, nil
}

func (f *fakeGuildClient) GetGuild(_ context.Context, req *guildv1.GetGuildRequest, _ ...grpc.CallOption) (*guildv1.GetGuildResponse, error) {
	f.getGuildReq = req
	if f.getGuildFn != nil {
		return f.getGuildFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) ListUserGuilds(_ context.Context, req *guildv1.ListUserGuildsRequest, _ ...grpc.CallOption) (*guildv1.ListUserGuildsResponse, error) {
	f.listGuildsReq = req
	if f.listGuildsFn != nil {
		return f.listGuildsFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) DeleteGuild(_ context.Context, req *guildv1.DeleteGuildRequest, _ ...grpc.CallOption) (*guildv1.DeleteGuildResponse, error) {
	f.deleteReq = req
	if f.deleteFn != nil {
		return f.deleteFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) GetGuildMember(_ context.Context, req *guildv1.GetGuildMemberRequest, _ ...grpc.CallOption) (*guildv1.GetGuildMemberResponse, error) {
	f.getMemberReq = req
	if f.getMemberFn != nil {
		return f.getMemberFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) ListGuildMembers(_ context.Context, req *guildv1.ListGuildMembersRequest, _ ...grpc.CallOption) (*guildv1.ListGuildMembersResponse, error) {
	f.listMembersReq = req
	if f.listMembersFn != nil {
		return f.listMembersFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) KickGuildMember(_ context.Context, req *guildv1.KickGuildMemberRequest, _ ...grpc.CallOption) (*guildv1.KickGuildMemberResponse, error) {
	f.kickReq = req
	if f.kickFn != nil {
		return f.kickFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) BanGuildMember(_ context.Context, req *guildv1.BanGuildMemberRequest, _ ...grpc.CallOption) (*guildv1.BanGuildMemberResponse, error) {
	f.banReq = req
	if f.banFn != nil {
		return f.banFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) UnbanGuildMember(_ context.Context, req *guildv1.UnbanGuildMemberRequest, _ ...grpc.CallOption) (*guildv1.UnbanGuildMemberResponse, error) {
	f.unbanReq = req
	if f.unbanFn != nil {
		return f.unbanFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) ListGuildBans(_ context.Context, req *guildv1.ListGuildBansRequest, _ ...grpc.CallOption) (*guildv1.ListGuildBansResponse, error) {
	f.listBansReq = req
	if f.listBansFn != nil {
		return f.listBansFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) GetGuildRole(_ context.Context, req *guildv1.GetGuildRoleRequest, _ ...grpc.CallOption) (*guildv1.GetGuildRoleResponse, error) {
	f.getRoleReq = req
	if f.getRoleFn != nil {
		return f.getRoleFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) ListGuildRoles(_ context.Context, req *guildv1.ListGuildRolesRequest, _ ...grpc.CallOption) (*guildv1.ListGuildRolesResponse, error) {
	f.listRolesReq = req
	if f.listRolesFn != nil {
		return f.listRolesFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) UpdateGuildRole(_ context.Context, req *guildv1.UpdateGuildRoleRequest, _ ...grpc.CallOption) (*guildv1.UpdateGuildRoleResponse, error) {
	f.updateRoleReq = req
	if f.updateRoleFn != nil {
		return f.updateRoleFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) DeleteGuildRole(_ context.Context, req *guildv1.DeleteGuildRoleRequest, _ ...grpc.CallOption) (*guildv1.DeleteGuildRoleResponse, error) {
	f.deleteRoleReq = req
	if f.deleteRoleFn != nil {
		return f.deleteRoleFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) ReorderGuildRoles(_ context.Context, req *guildv1.ReorderGuildRolesRequest, _ ...grpc.CallOption) (*guildv1.ReorderGuildRolesResponse, error) {
	f.reorderRolesReq = req
	if f.reorderRolesFn != nil {
		return f.reorderRolesFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) AddGuildMemberRole(_ context.Context, req *guildv1.AddGuildMemberRoleRequest, _ ...grpc.CallOption) (*guildv1.AddGuildMemberRoleResponse, error) {
	f.addMemberRoleReq = req
	if f.addMemberRoleFn != nil {
		return f.addMemberRoleFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) RemoveGuildMemberRole(_ context.Context, req *guildv1.RemoveGuildMemberRoleRequest, _ ...grpc.CallOption) (*guildv1.RemoveGuildMemberRoleResponse, error) {
	f.removeMemberRoleReq = req
	if f.removeMemberRoleFn != nil {
		return f.removeMemberRoleFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) ListGuildMemberRoles(_ context.Context, req *guildv1.ListGuildMemberRolesRequest, _ ...grpc.CallOption) (*guildv1.ListGuildMemberRolesResponse, error) {
	f.listMemberRolesReq = req
	if f.listMemberRolesFn != nil {
		return f.listMemberRolesFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) GetGuildMemberPermissions(_ context.Context, req *guildv1.GetGuildMemberPermissionsRequest, _ ...grpc.CallOption) (*guildv1.GetGuildMemberPermissionsResponse, error) {
	f.permissionsReq = req
	if f.permissionsFn != nil {
		return f.permissionsFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) GetGuildChannel(_ context.Context, req *guildv1.GetGuildChannelRequest, _ ...grpc.CallOption) (*guildv1.GetGuildChannelResponse, error) {
	f.getChannelReq = req
	if f.getChannelFn != nil {
		return f.getChannelFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) ListGuildChannels(_ context.Context, req *guildv1.ListGuildChannelsRequest, _ ...grpc.CallOption) (*guildv1.ListGuildChannelsResponse, error) {
	f.listChannelsReq = req
	if f.listChannelsFn != nil {
		return f.listChannelsFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) UpdateGuildChannel(_ context.Context, req *guildv1.UpdateGuildChannelRequest, _ ...grpc.CallOption) (*guildv1.UpdateGuildChannelResponse, error) {
	f.updateChannelReq = req
	if f.updateChannelFn != nil {
		return f.updateChannelFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) DeleteGuildChannel(_ context.Context, req *guildv1.DeleteGuildChannelRequest, _ ...grpc.CallOption) (*guildv1.DeleteGuildChannelResponse, error) {
	f.deleteChannelReq = req
	if f.deleteChannelFn != nil {
		return f.deleteChannelFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) ReorderGuildChannels(_ context.Context, req *guildv1.ReorderGuildChannelsRequest, _ ...grpc.CallOption) (*guildv1.ReorderGuildChannelsResponse, error) {
	f.reorderChannelsReq = req
	if f.reorderChannelsFn != nil {
		return f.reorderChannelsFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) UpsertGuildChannelPermissionOverwrite(_ context.Context, req *guildv1.UpsertGuildChannelPermissionOverwriteRequest, _ ...grpc.CallOption) (*guildv1.UpsertGuildChannelPermissionOverwriteResponse, error) {
	f.upsertOverwriteReq = req
	if f.upsertOverwriteFn != nil {
		return f.upsertOverwriteFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) DeleteGuildChannelPermissionOverwrite(_ context.Context, req *guildv1.DeleteGuildChannelPermissionOverwriteRequest, _ ...grpc.CallOption) (*guildv1.DeleteGuildChannelPermissionOverwriteResponse, error) {
	f.deleteOverwriteReq = req
	if f.deleteOverwriteFn != nil {
		return f.deleteOverwriteFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) ListGuildChannelPermissionOverwrites(_ context.Context, req *guildv1.ListGuildChannelPermissionOverwritesRequest, _ ...grpc.CallOption) (*guildv1.ListGuildChannelPermissionOverwritesResponse, error) {
	f.listOverwritesReq = req
	if f.listOverwritesFn != nil {
		return f.listOverwritesFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) CreateGuild(_ context.Context, req *guildv1.CreateGuildRequest, _ ...grpc.CallOption) (*guildv1.CreateGuildResponse, error) {
	f.createRequest = req
	return f.createResponse, nil
}

func TestGuildMemberMutationsUseAuthenticatedActor(t *testing.T) {
	member := internalGuildMember()
	addResp := new(guildv1.AddGuildMemberResponse)
	addResp.SetMember(member)
	updateResp := new(guildv1.UpdateGuildMemberResponse)
	updateResp.SetMember(member)
	leaveResp := new(guildv1.LeaveGuildResponse)
	leaveResp.SetOk(true)
	transferResp := new(guildv1.TransferGuildOwnershipResponse)
	transferResp.SetGuild(internalGuild())
	guildClient := &fakeGuildClient{
		addMemberResponse:    addResp,
		updateMemberResponse: updateResp,
		leaveResponse:        leaveResp,
		transferResponse:     transferResp,
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	_, err := client.AddGuildMember(context.Background(), &apiv1.AddGuildMemberRequest{
		GuildId: new(int64(3001)), UserId: new(int64(1002)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.addMemberRequest.GetActorUserId())
	require.Equal(t, int64(1002), guildClient.addMemberRequest.GetUserId())

	_, err = client.UpdateCurrentGuildMember(context.Background(), &apiv1.UpdateCurrentGuildMemberRequest{
		GuildId: new(int64(3001)), Nickname: new("member"),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.updateMemberRequest.GetActorUserId())

	_, err = client.LeaveGuild(context.Background(), &apiv1.LeaveGuildRequest{GuildId: new(int64(3001))})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.leaveRequest.GetUserId())

	_, err = client.TransferGuildOwnership(context.Background(), &apiv1.TransferGuildOwnershipRequest{
		GuildId: new(int64(3001)), NewOwnerId: new(int64(1002)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.transferRequest.GetActorUserId())
	require.Equal(t, int64(1002), guildClient.transferRequest.GetNewOwnerId())
}

func (f *fakeGuildClient) UpdateGuild(_ context.Context, req *guildv1.UpdateGuildRequest, _ ...grpc.CallOption) (*guildv1.UpdateGuildResponse, error) {
	f.updateRequest = req
	return f.updateResponse, nil
}

func TestCreateGuildUsesAuthenticatedOwner(t *testing.T) {
	internal := internalGuild()
	createResp := new(guildv1.CreateGuildResponse)
	createResp.SetGuild(internal)
	guildClient := &fakeGuildClient{createResponse: createResp}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.CreateGuild(context.Background(), &apiv1.CreateGuildRequest{
		Name: new("Cordis"), IconUri: new("icon://guild"),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.createRequest.GetOwnerId())
	require.Equal(t, "Cordis", guildClient.createRequest.GetName())
	require.Equal(t, int64(3001), resp.GetGuild().GetId())
}

func TestUpdateGuildUsesAuthenticatedActorAndFieldPresence(t *testing.T) {
	updateResp := new(guildv1.UpdateGuildResponse)
	updateResp.SetGuild(internalGuild())
	guildClient := &fakeGuildClient{updateResponse: updateResp}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	_, err := client.UpdateGuild(context.Background(), &apiv1.UpdateGuildRequest{
		GuildId: new(int64(3001)), IconUri: new(""),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.updateRequest.GetActorUserId())
	require.False(t, guildClient.updateRequest.HasName())
	require.True(t, guildClient.updateRequest.HasIconUri())
	require.Empty(t, guildClient.updateRequest.GetIconUri())
}

func TestCreateGuildRoleUsesAuthenticatedActor(t *testing.T) {
	role := new(guildv1.GuildRole)
	role.SetId(4001)
	role.SetGuildId(3001)
	role.SetName("moderator")
	role.SetPermissions(16)
	resp := new(guildv1.CreateGuildRoleResponse)
	resp.SetRole(role)
	guildClient := &fakeGuildClient{createRoleResponse: resp}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	result, err := client.CreateGuildRole(context.Background(), &apiv1.CreateGuildRoleRequest{
		GuildId: new(int64(3001)), Name: new("moderator"), Permissions: new(uint64(16)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.createRoleRequest.GetActorUserId())
	require.Equal(t, uint64(16), guildClient.createRoleRequest.GetPermissions())
	require.Equal(t, int64(4001), result.GetRole().GetId())
}

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

	result, err := client.CreateGuildChannel(context.Background(), &apiv1.CreateGuildChannelRequest{
		GuildId: new(int64(3001)), Name: new("general"),
		Type: new(apiv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.createChannelRequest.GetActorUserId())
	require.Equal(t, int64(5001), result.GetChannel().GetId())
}

func TestGetGuildUsesAuthenticatedUser(t *testing.T) {
	guildClient := &fakeGuildClient{
		getGuildFn: func(*guildv1.GetGuildRequest) (*guildv1.GetGuildResponse, error) {
			resp := new(guildv1.GetGuildResponse)
			resp.SetGuild(internalGuild())
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.GetGuild(context.Background(), &apiv1.GetGuildRequest{GuildId: new(int64(3001))})
	require.NoError(t, err)
	require.Equal(t, int64(3001), guildClient.getGuildReq.GetGuildId())
	require.Equal(t, int64(1001), guildClient.getGuildReq.GetUserId())
	require.Equal(t, int64(3001), resp.GetGuild().GetId())
}

func TestListGuildsUsesAuthenticatedUser(t *testing.T) {
	guildClient := &fakeGuildClient{
		listGuildsFn: func(*guildv1.ListUserGuildsRequest) (*guildv1.ListUserGuildsResponse, error) {
			resp := new(guildv1.ListUserGuildsResponse)
			resp.SetGuilds([]*guildv1.Guild{internalGuild()})
			resp.SetBeforeCursor(3000)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.ListGuilds(context.Background(), &apiv1.ListGuildsRequest{
		Before: new(int64(3000)), Limit: new(int32(25)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.listGuildsReq.GetUserId())
	require.Equal(t, int64(3000), guildClient.listGuildsReq.GetBefore())
	require.Equal(t, int32(25), guildClient.listGuildsReq.GetLimit())
	require.Len(t, resp.GetGuilds(), 1)
	require.Equal(t, int64(3000), resp.GetBeforeCursor())
}

func TestDeleteGuildUsesAuthenticatedActor(t *testing.T) {
	guildClient := &fakeGuildClient{
		deleteFn: func(*guildv1.DeleteGuildRequest) (*guildv1.DeleteGuildResponse, error) {
			resp := new(guildv1.DeleteGuildResponse)
			resp.SetOk(true)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.DeleteGuild(context.Background(), &apiv1.DeleteGuildRequest{GuildId: new(int64(3001))})
	require.NoError(t, err)
	require.Equal(t, int64(3001), guildClient.deleteReq.GetGuildId())
	require.Equal(t, int64(1001), guildClient.deleteReq.GetActorUserId())
	require.True(t, resp.GetOk())
}

func TestGetGuildMemberUsesAuthenticatedActor(t *testing.T) {
	guildClient := &fakeGuildClient{
		getMemberFn: func(*guildv1.GetGuildMemberRequest) (*guildv1.GetGuildMemberResponse, error) {
			resp := new(guildv1.GetGuildMemberResponse)
			resp.SetMember(internalGuildMember())
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.GetGuildMember(context.Background(), &apiv1.GetGuildMemberRequest{
		GuildId: new(int64(3001)), UserId: new(int64(1002)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.getMemberReq.GetActorUserId())
	require.Equal(t, int64(1002), guildClient.getMemberReq.GetUserId())
	require.Equal(t, int64(1001), resp.GetMember().GetUserId()) // returns internalGuildMember which has UserId=1001
}

func TestListGuildMembersMapsRequestAndResponse(t *testing.T) {
	member := internalGuildMember()
	guildClient := &fakeGuildClient{
		listMembersFn: func(*guildv1.ListGuildMembersRequest) (*guildv1.ListGuildMembersResponse, error) {
			resp := new(guildv1.ListGuildMembersResponse)
			resp.SetMembers([]*guildv1.GuildMember{member})
			resp.SetBeforeUserId(1000)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.ListGuildMembers(context.Background(), &apiv1.ListGuildMembersRequest{
		GuildId: new(int64(3001)), BeforeUserId: new(int64(1002)), Limit: new(int32(50)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.listMembersReq.GetActorUserId())
	require.Equal(t, int64(1002), guildClient.listMembersReq.GetBeforeUserId())
	require.Equal(t, int32(50), guildClient.listMembersReq.GetLimit())
	require.Len(t, resp.GetMembers(), 1)
	require.Equal(t, int64(1000), resp.GetBeforeUserId())
}

func TestKickGuildMemberUsesAuthenticatedActor(t *testing.T) {
	guildClient := &fakeGuildClient{
		kickFn: func(*guildv1.KickGuildMemberRequest) (*guildv1.KickGuildMemberResponse, error) {
			resp := new(guildv1.KickGuildMemberResponse)
			resp.SetOk(true)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.KickGuildMember(context.Background(), &apiv1.KickGuildMemberRequest{
		GuildId: new(int64(3001)), UserId: new(int64(1002)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.kickReq.GetActorUserId())
	require.Equal(t, int64(1002), guildClient.kickReq.GetUserId())
	require.True(t, resp.GetOk())
}

func TestBanGuildMemberMapsRequestAndResponse(t *testing.T) {
	ban := new(guildv1.GuildBan)
	ban.SetGuildId(3001)
	ban.SetUserId(1002)
	ban.SetActorUserId(1001)
	ban.SetReason("spam")
	ban.SetCreatedAt(4001)
	guildClient := &fakeGuildClient{
		banFn: func(*guildv1.BanGuildMemberRequest) (*guildv1.BanGuildMemberResponse, error) {
			resp := new(guildv1.BanGuildMemberResponse)
			resp.SetBan(ban)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.BanGuildMember(context.Background(), &apiv1.BanGuildMemberRequest{
		GuildId: new(int64(3001)), UserId: new(int64(1002)), Reason: new("spam"),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.banReq.GetActorUserId())
	require.Equal(t, int64(1002), guildClient.banReq.GetUserId())
	require.Equal(t, "spam", guildClient.banReq.GetReason())
	require.Equal(t, int64(1002), resp.GetBan().GetUserId())
}

func TestUnbanGuildMemberUsesAuthenticatedActor(t *testing.T) {
	guildClient := &fakeGuildClient{
		unbanFn: func(*guildv1.UnbanGuildMemberRequest) (*guildv1.UnbanGuildMemberResponse, error) {
			resp := new(guildv1.UnbanGuildMemberResponse)
			resp.SetOk(true)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.UnbanGuildMember(context.Background(), &apiv1.UnbanGuildMemberRequest{
		GuildId: new(int64(3001)), UserId: new(int64(1002)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.unbanReq.GetActorUserId())
	require.True(t, resp.GetOk())
}

func TestListGuildBansMapsRequestAndResponse(t *testing.T) {
	ban := new(guildv1.GuildBan)
	ban.SetGuildId(3001)
	ban.SetUserId(1002)
	ban.SetCreatedAt(4001)
	guildClient := &fakeGuildClient{
		listBansFn: func(*guildv1.ListGuildBansRequest) (*guildv1.ListGuildBansResponse, error) {
			resp := new(guildv1.ListGuildBansResponse)
			resp.SetBans([]*guildv1.GuildBan{ban})
			resp.SetBeforeUserId(1003)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.ListGuildBans(context.Background(), &apiv1.ListGuildBansRequest{
		GuildId: new(int64(3001)), BeforeUserId: new(int64(1003)), Limit: new(int32(20)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.listBansReq.GetActorUserId())
	require.Len(t, resp.GetBans(), 1)
	require.Equal(t, int64(1003), resp.GetBeforeUserId())
}

func TestGetGuildRoleMapsRequestAndResponse(t *testing.T) {
	guildClient := &fakeGuildClient{
		getRoleFn: func(*guildv1.GetGuildRoleRequest) (*guildv1.GetGuildRoleResponse, error) {
			resp := new(guildv1.GetGuildRoleResponse)
			resp.SetRole(internalGuildRole())
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.GetGuildRole(context.Background(), &apiv1.GetGuildRoleRequest{
		GuildId: new(int64(3001)), RoleId: new(int64(4001)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.getRoleReq.GetActorUserId())
	require.Equal(t, int64(4001), guildClient.getRoleReq.GetRoleId())
	require.Equal(t, int64(4001), resp.GetRole().GetId())
}

func TestListGuildRolesMapsResponse(t *testing.T) {
	guildClient := &fakeGuildClient{
		listRolesFn: func(*guildv1.ListGuildRolesRequest) (*guildv1.ListGuildRolesResponse, error) {
			resp := new(guildv1.ListGuildRolesResponse)
			resp.SetRoles([]*guildv1.GuildRole{internalGuildRole()})
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.ListGuildRoles(context.Background(), &apiv1.ListGuildRolesRequest{GuildId: new(int64(3001))})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.listRolesReq.GetActorUserId())
	require.Len(t, resp.GetRoles(), 1)
}

func TestUpdateGuildRolePreservesFieldPresence(t *testing.T) {
	guildClient := &fakeGuildClient{
		updateRoleFn: func(*guildv1.UpdateGuildRoleRequest) (*guildv1.UpdateGuildRoleResponse, error) {
			resp := new(guildv1.UpdateGuildRoleResponse)
			resp.SetRole(internalGuildRole())
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	_, err := client.UpdateGuildRole(context.Background(), &apiv1.UpdateGuildRoleRequest{
		GuildId: new(int64(3001)), RoleId: new(int64(4001)), Permissions: new(uint64(32)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.updateRoleReq.GetActorUserId())
	require.False(t, guildClient.updateRoleReq.HasName())
	require.True(t, guildClient.updateRoleReq.HasPermissions())
	require.Equal(t, uint64(32), guildClient.updateRoleReq.GetPermissions())
}

func TestDeleteGuildRoleMapsRequest(t *testing.T) {
	guildClient := &fakeGuildClient{
		deleteRoleFn: func(*guildv1.DeleteGuildRoleRequest) (*guildv1.DeleteGuildRoleResponse, error) {
			resp := new(guildv1.DeleteGuildRoleResponse)
			resp.SetOk(true)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.DeleteGuildRole(context.Background(), &apiv1.DeleteGuildRoleRequest{
		GuildId: new(int64(3001)), RoleId: new(int64(4001)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.deleteRoleReq.GetActorUserId())
	require.True(t, resp.GetOk())
}

func TestReorderGuildRolesMapsPositions(t *testing.T) {
	guildClient := &fakeGuildClient{
		reorderRolesFn: func(*guildv1.ReorderGuildRolesRequest) (*guildv1.ReorderGuildRolesResponse, error) {
			resp := new(guildv1.ReorderGuildRolesResponse)
			resp.SetRoles([]*guildv1.GuildRole{internalGuildRole()})
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.ReorderGuildRoles(context.Background(), &apiv1.ReorderGuildRolesRequest{
		GuildId: new(int64(3001)),
		Positions: []*apiv1.GuildRolePosition{
			{RoleId: new(int64(4001)), Position: new(int32(0))},
			{RoleId: new(int64(4002)), Position: new(int32(1))},
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.GetRoles(), 1)
	require.Equal(t, int64(1001), guildClient.reorderRolesReq.GetActorUserId())
	require.Len(t, guildClient.reorderRolesReq.GetPositions(), 2)
	require.Equal(t, int64(4001), guildClient.reorderRolesReq.GetPositions()[0].GetRoleId())
	require.Equal(t, int32(1), guildClient.reorderRolesReq.GetPositions()[1].GetPosition())
}

func TestAddGuildMemberRoleUsesAuthenticatedActor(t *testing.T) {
	guildClient := &fakeGuildClient{
		addMemberRoleFn: func(*guildv1.AddGuildMemberRoleRequest) (*guildv1.AddGuildMemberRoleResponse, error) {
			resp := new(guildv1.AddGuildMemberRoleResponse)
			resp.SetOk(true)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.AddGuildMemberRole(context.Background(), &apiv1.AddGuildMemberRoleRequest{
		GuildId: new(int64(3001)), UserId: new(int64(1002)), RoleId: new(int64(4001)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.addMemberRoleReq.GetActorUserId())
	require.Equal(t, int64(1002), guildClient.addMemberRoleReq.GetUserId())
	require.Equal(t, int64(4001), guildClient.addMemberRoleReq.GetRoleId())
	require.True(t, resp.GetOk())
}

func TestRemoveGuildMemberRoleUsesAuthenticatedActor(t *testing.T) {
	guildClient := &fakeGuildClient{
		removeMemberRoleFn: func(*guildv1.RemoveGuildMemberRoleRequest) (*guildv1.RemoveGuildMemberRoleResponse, error) {
			resp := new(guildv1.RemoveGuildMemberRoleResponse)
			resp.SetOk(true)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.RemoveGuildMemberRole(context.Background(), &apiv1.RemoveGuildMemberRoleRequest{
		GuildId: new(int64(3001)), UserId: new(int64(1002)), RoleId: new(int64(4001)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.removeMemberRoleReq.GetActorUserId())
	require.True(t, resp.GetOk())
}

func TestListGuildMemberRolesMapsResponse(t *testing.T) {
	guildClient := &fakeGuildClient{
		listMemberRolesFn: func(*guildv1.ListGuildMemberRolesRequest) (*guildv1.ListGuildMemberRolesResponse, error) {
			resp := new(guildv1.ListGuildMemberRolesResponse)
			resp.SetRoles([]*guildv1.GuildRole{internalGuildRole()})
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.ListGuildMemberRoles(context.Background(), &apiv1.ListGuildMemberRolesRequest{
		GuildId: new(int64(3001)), UserId: new(int64(1002)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.listMemberRolesReq.GetActorUserId())
	require.Len(t, resp.GetRoles(), 1)
}

func TestGetGuildMemberPermissionsMapsResponse(t *testing.T) {
	guildClient := &fakeGuildClient{
		permissionsFn: func(*guildv1.GetGuildMemberPermissionsRequest) (*guildv1.GetGuildMemberPermissionsResponse, error) {
			resp := new(guildv1.GetGuildMemberPermissionsResponse)
			resp.SetPermissions(42)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.GetGuildMemberPermissions(context.Background(), &apiv1.GetGuildMemberPermissionsRequest{
		GuildId: new(int64(3001)), UserId: new(int64(1002)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.permissionsReq.GetActorUserId())
	require.Equal(t, int64(1002), guildClient.permissionsReq.GetUserId())
	require.Equal(t, uint64(42), resp.GetPermissions())
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

	resp, err := client.GetGuildChannel(context.Background(), &apiv1.GetGuildChannelRequest{ChannelId: new(int64(5001))})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.getChannelReq.GetActorUserId())
	require.Equal(t, int64(5001), resp.GetChannel().GetId())
}

func TestListGuildChannelsMapsResponse(t *testing.T) {
	guildClient := &fakeGuildClient{
		listChannelsFn: func(*guildv1.ListGuildChannelsRequest) (*guildv1.ListGuildChannelsResponse, error) {
			resp := new(guildv1.ListGuildChannelsResponse)
			resp.SetChannels([]*guildv1.GuildChannel{internalGuildChannel()})
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.ListGuildChannels(context.Background(), &apiv1.ListGuildChannelsRequest{GuildId: new(int64(3001))})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.listChannelsReq.GetActorUserId())
	require.Len(t, resp.GetChannels(), 1)
}

func TestUpdateGuildChannelPreservesFieldPresence(t *testing.T) {
	guildClient := &fakeGuildClient{
		updateChannelFn: func(*guildv1.UpdateGuildChannelRequest) (*guildv1.UpdateGuildChannelResponse, error) {
			resp := new(guildv1.UpdateGuildChannelResponse)
			resp.SetChannel(internalGuildChannel())
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	_, err := client.UpdateGuildChannel(context.Background(), &apiv1.UpdateGuildChannelRequest{
		ChannelId: new(int64(5001)), Name: new("renamed"), ParentId: new(int64(0)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.updateChannelReq.GetActorUserId())
	require.True(t, guildClient.updateChannelReq.HasName())
	require.False(t, guildClient.updateChannelReq.HasTopic())
	require.True(t, guildClient.updateChannelReq.HasParentId())
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

	resp, err := client.DeleteGuildChannel(context.Background(), &apiv1.DeleteGuildChannelRequest{ChannelId: new(int64(5001))})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.deleteChannelReq.GetActorUserId())
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

	resp, err := client.ReorderGuildChannels(context.Background(), &apiv1.ReorderGuildChannelsRequest{
		GuildId: new(int64(3001)),
		Positions: []*apiv1.GuildChannelPosition{
			{ChannelId: new(int64(5001)), Position: new(int32(0))},
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.GetChannels(), 1)
	require.Equal(t, int64(1001), guildClient.reorderChannelsReq.GetActorUserId())
	require.Len(t, guildClient.reorderChannelsReq.GetPositions(), 1)
	require.Equal(t, int64(5001), guildClient.reorderChannelsReq.GetPositions()[0].GetChannelId())
}

func TestUpsertGuildChannelPermissionOverwriteMapsRequestAndResponse(t *testing.T) {
	overwrite := new(guildv1.GuildChannelPermissionOverwrite)
	overwrite.SetChannelId(5001)
	overwrite.SetGuildId(3001)
	overwrite.SetTargetType(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE)
	overwrite.SetTargetId(4001)
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

	resp, err := client.UpsertGuildChannelPermissionOverwrite(context.Background(), &apiv1.UpsertGuildChannelPermissionOverwriteRequest{
		ChannelId:  new(int64(5001)),
		TargetType: new(apiv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE),
		TargetId:   new(int64(4001)),
		Allow:      new(uint64(8)),
		Deny:       new(uint64(4)),
	})
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

	resp, err := client.DeleteGuildChannelPermissionOverwrite(context.Background(), &apiv1.DeleteGuildChannelPermissionOverwriteRequest{
		ChannelId:  new(int64(5001)),
		TargetType: new(apiv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER),
		TargetId:   new(int64(1002)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.deleteOverwriteReq.GetActorUserId())
	require.True(t, resp.GetOk())
}

func TestListGuildChannelPermissionOverwritesMapsResponse(t *testing.T) {
	overwrite := new(guildv1.GuildChannelPermissionOverwrite)
	overwrite.SetChannelId(5001)
	overwrite.SetGuildId(3001)
	overwrite.SetTargetId(4001)
	guildClient := &fakeGuildClient{
		listOverwritesFn: func(*guildv1.ListGuildChannelPermissionOverwritesRequest) (*guildv1.ListGuildChannelPermissionOverwritesResponse, error) {
			resp := new(guildv1.ListGuildChannelPermissionOverwritesResponse)
			resp.SetOverwrites([]*guildv1.GuildChannelPermissionOverwrite{overwrite})
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.ListGuildChannelPermissionOverwrites(context.Background(), &apiv1.ListGuildChannelPermissionOverwritesRequest{
		ChannelId: new(int64(5001)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.listOverwritesReq.GetActorUserId())
	require.Len(t, resp.GetOverwrites(), 1)
}

func TestGuildErrorMappings(t *testing.T) {
	tests := map[string]struct {
		fn          func(*fakeGuildClient) error
		connectCode connect.Code
		publicCode  string
	}{
		"not found": {
			fn: func(f *fakeGuildClient) error {
				f.getGuildFn = func(*guildv1.GetGuildRequest) (*guildv1.GetGuildResponse, error) {
					return nil, rpcerror.New(codes.NotFound, rpcerror.GuildDomain, rpcerror.GuildNotFound, "guild not found")
				}
				client, closeServer := newGuildHTTPClient(t, f)
				defer closeServer()
				_, err := client.GetGuild(context.Background(), &apiv1.GetGuildRequest{GuildId: new(int64(3001))})
				return err
			},
			connectCode: connect.CodeNotFound,
			publicCode:  apierror.CodeNotFound,
		},
		"permission denied": {
			fn: func(f *fakeGuildClient) error {
				f.updateRoleFn = func(*guildv1.UpdateGuildRoleRequest) (*guildv1.UpdateGuildRoleResponse, error) {
					return nil, rpcerror.New(codes.PermissionDenied, rpcerror.GuildDomain, rpcerror.GuildPermissionDenied, "permission denied")
				}
				client, closeServer := newGuildHTTPClient(t, f)
				defer closeServer()
				_, err := client.UpdateGuildRole(context.Background(), &apiv1.UpdateGuildRoleRequest{
					GuildId: new(int64(3001)), RoleId: new(int64(4001)), Name: new("test"),
				})
				return err
			},
			connectCode: connect.CodePermissionDenied,
			publicCode:  apierror.CodePermissionDenied,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tt.fn(&fakeGuildClient{})
			require.Equal(t, tt.connectCode, connect.CodeOf(err))
			require.Equal(t, tt.publicCode, publicErrorInfo(t, err).GetCode())
		})
	}
}

func newGuildHTTPClient(t *testing.T, guildClient *fakeGuildClient) (apiv1connect.GuildServiceClient, func()) {
	t.Helper()
	svcCtx := &svc.ServiceContext{
		AuthenticatorClient: &fakeAuthenticatorClient{verifyResponse: verifyAccessTokenResponse(1001)},
		GuildClient:         guildClient,
	}
	path, handler := apiv1connect.NewGuildServiceHandler(NewGuild(svcCtx))
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	httpServer := httptest.NewServer(mux)
	httpClient := &http.Client{Transport: bearerRoundTripper{base: http.DefaultTransport, accessToken: "access-token"}}
	return apiv1connect.NewGuildServiceClient(httpClient, httpServer.URL), httpServer.Close
}

func internalGuild() *guildv1.Guild {
	guild := new(guildv1.Guild)
	guild.SetId(3001)
	guild.SetOwnerId(1001)
	guild.SetName("Cordis")
	guild.SetIconUri("icon://guild")
	guild.SetRevision(1)
	guild.SetCreatedAt(4001)
	return guild
}

func internalGuildMember() *guildv1.GuildMember {
	member := new(guildv1.GuildMember)
	member.SetGuildId(3001)
	member.SetUserId(1001)
	member.SetNickname("member")
	member.SetRevision(2)
	member.SetJoinedAt(4001)
	member.SetUpdatedAt(4002)
	return member
}

func internalGuildRole() *guildv1.GuildRole {
	role := new(guildv1.GuildRole)
	role.SetId(4001)
	role.SetGuildId(3001)
	role.SetName("moderator")
	role.SetPermissions(16)
	role.SetPosition(1)
	role.SetRevision(3)
	role.SetCreatedAt(4000)
	role.SetUpdatedAt(4001)
	return role
}

func internalGuildChannel() *guildv1.GuildChannel {
	channel := new(guildv1.GuildChannel)
	channel.SetId(5001)
	channel.SetGuildId(3001)
	channel.SetName("general")
	channel.SetType(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT)
	channel.SetPosition(0)
	channel.SetTopic("topic")
	channel.SetRevision(4)
	channel.SetCreatedAt(4000)
	channel.SetUpdatedAt(4001)
	channel.SetParentId(0)
	return channel
}
