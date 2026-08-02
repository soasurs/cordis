package server

import (
	"context"

	"google.golang.org/grpc"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
)

type fakeGuildClient struct {
	guildv1.GuildServiceClient
	createRequest         *guildv1.CreateGuildRequest
	createFn              func(*guildv1.CreateGuildRequest) (*guildv1.CreateGuildResponse, error)
	updateRequest         *guildv1.UpdateGuildRequest
	createIconRequest     *guildv1.CreateGuildIconUploadRequest
	createIconResponse    *guildv1.CreateGuildIconUploadResponse
	createIconError       error
	addMemberRequest      *guildv1.AddGuildMemberRequest
	updateMemberRequest   *guildv1.UpdateGuildMemberRequest
	leaveRequest          *guildv1.LeaveGuildRequest
	transferRequest       *guildv1.TransferGuildOwnershipRequest
	createRoleRequest     *guildv1.CreateGuildRoleRequest
	createRoleFn          func(*guildv1.CreateGuildRoleRequest) (*guildv1.CreateGuildRoleResponse, error)
	createChannelRequest  *guildv1.CreateGuildChannelRequest
	createChannelFn       func(*guildv1.CreateGuildChannelRequest) (*guildv1.CreateGuildChannelResponse, error)
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

	getRoleReq           *guildv1.GetGuildRoleRequest
	getRoleFn            func(*guildv1.GetGuildRoleRequest) (*guildv1.GetGuildRoleResponse, error)
	listRolesReq         *guildv1.ListGuildRolesRequest
	listRolesFn          func(*guildv1.ListGuildRolesRequest) (*guildv1.ListGuildRolesResponse, error)
	updateRoleReq        *guildv1.UpdateGuildRoleRequest
	updateRoleFn         func(*guildv1.UpdateGuildRoleRequest) (*guildv1.UpdateGuildRoleResponse, error)
	deleteRoleReq        *guildv1.DeleteGuildRoleRequest
	deleteRoleFn         func(*guildv1.DeleteGuildRoleRequest) (*guildv1.DeleteGuildRoleResponse, error)
	reorderRolesReq      *guildv1.ReorderGuildRolesRequest
	reorderRolesFn       func(*guildv1.ReorderGuildRolesRequest) (*guildv1.ReorderGuildRolesResponse, error)
	addMemberRoleReq     *guildv1.AddGuildMemberRoleRequest
	addMemberRoleFn      func(*guildv1.AddGuildMemberRoleRequest) (*guildv1.AddGuildMemberRoleResponse, error)
	removeMemberRoleReq  *guildv1.RemoveGuildMemberRoleRequest
	removeMemberRoleFn   func(*guildv1.RemoveGuildMemberRoleRequest) (*guildv1.RemoveGuildMemberRoleResponse, error)
	addRoleMembersReq    *guildv1.AddGuildRoleMembersRequest
	addRoleMembersFn     func(*guildv1.AddGuildRoleMembersRequest) (*guildv1.AddGuildRoleMembersResponse, error)
	removeRoleMembersReq *guildv1.RemoveGuildRoleMembersRequest
	removeRoleMembersFn  func(*guildv1.RemoveGuildRoleMembersRequest) (*guildv1.RemoveGuildRoleMembersResponse, error)
	listMemberRolesReq   *guildv1.ListGuildMemberRolesRequest
	listMemberRolesFn    func(*guildv1.ListGuildMemberRolesRequest) (*guildv1.ListGuildMemberRolesResponse, error)
	listRoleMembersReq   *guildv1.ListGuildRoleMembersRequest
	listRoleMembersFn    func(*guildv1.ListGuildRoleMembersRequest) (*guildv1.ListGuildRoleMembersResponse, error)
	permissionsReq       *guildv1.GetGuildMemberPermissionsRequest
	permissionsFn        func(*guildv1.GetGuildMemberPermissionsRequest) (*guildv1.GetGuildMemberPermissionsResponse, error)

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

	searchUsersReq  *guildv1.SearchGuildMentionUsersRequest
	searchUsersFn   func(*guildv1.SearchGuildMentionUsersRequest) (*guildv1.SearchGuildMentionUsersResponse, error)
	searchUsersResp *guildv1.SearchGuildMentionUsersResponse
	searchRolesReq  *guildv1.SearchGuildMentionRolesRequest
	searchRolesFn   func(*guildv1.SearchGuildMentionRolesRequest) (*guildv1.SearchGuildMentionRolesResponse, error)
	searchRolesResp *guildv1.SearchGuildMentionRolesResponse
}

func (f *fakeGuildClient) CreateGuildRole(_ context.Context, req *guildv1.CreateGuildRoleRequest, _ ...grpc.CallOption) (*guildv1.CreateGuildRoleResponse, error) {
	f.createRoleRequest = req
	if f.createRoleFn != nil {
		return f.createRoleFn(req)
	}
	return f.createRoleResponse, nil
}

func (f *fakeGuildClient) CreateGuildChannel(_ context.Context, req *guildv1.CreateGuildChannelRequest, _ ...grpc.CallOption) (*guildv1.CreateGuildChannelResponse, error) {
	f.createChannelRequest = req
	if f.createChannelFn != nil {
		return f.createChannelFn(req)
	}
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

func (f *fakeGuildClient) AddGuildRoleMembers(_ context.Context, req *guildv1.AddGuildRoleMembersRequest, _ ...grpc.CallOption) (*guildv1.AddGuildRoleMembersResponse, error) {
	f.addRoleMembersReq = req
	if f.addRoleMembersFn != nil {
		return f.addRoleMembersFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) RemoveGuildRoleMembers(_ context.Context, req *guildv1.RemoveGuildRoleMembersRequest, _ ...grpc.CallOption) (*guildv1.RemoveGuildRoleMembersResponse, error) {
	f.removeRoleMembersReq = req
	if f.removeRoleMembersFn != nil {
		return f.removeRoleMembersFn(req)
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

func (f *fakeGuildClient) ListGuildRoleMembers(_ context.Context, req *guildv1.ListGuildRoleMembersRequest, _ ...grpc.CallOption) (*guildv1.ListGuildRoleMembersResponse, error) {
	f.listRoleMembersReq = req
	if f.listRoleMembersFn != nil {
		return f.listRoleMembersFn(req)
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
	if f.createFn != nil {
		return f.createFn(req)
	}
	return f.createResponse, nil
}

func (f *fakeGuildClient) SearchGuildMentionUsers(_ context.Context, req *guildv1.SearchGuildMentionUsersRequest, _ ...grpc.CallOption) (*guildv1.SearchGuildMentionUsersResponse, error) {
	f.searchUsersReq = req
	if f.searchUsersFn != nil {
		return f.searchUsersFn(req)
	}
	return f.searchUsersResp, nil
}

func (f *fakeGuildClient) SearchGuildMentionRoles(_ context.Context, req *guildv1.SearchGuildMentionRolesRequest, _ ...grpc.CallOption) (*guildv1.SearchGuildMentionRolesResponse, error) {
	f.searchRolesReq = req
	if f.searchRolesFn != nil {
		return f.searchRolesFn(req)
	}
	return f.searchRolesResp, nil
}

func (f *fakeGuildClient) UpdateGuild(_ context.Context, req *guildv1.UpdateGuildRequest, _ ...grpc.CallOption) (*guildv1.UpdateGuildResponse, error) {
	f.updateRequest = req
	return f.updateResponse, nil
}

func (f *fakeGuildClient) CreateGuildIconUpload(
	_ context.Context,
	req *guildv1.CreateGuildIconUploadRequest,
	_ ...grpc.CallOption,
) (*guildv1.CreateGuildIconUploadResponse, error) {
	f.createIconRequest = req
	return f.createIconResponse, f.createIconError
}
