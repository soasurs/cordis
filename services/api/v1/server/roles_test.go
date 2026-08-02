package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
)

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

	createRoleReq := new(apiv1.CreateGuildRoleRequest)
	createRoleReq.SetGuildId(3001)
	createRoleReq.SetName("moderator")
	createRoleReq.SetPermissions(16)
	result, err := client.CreateGuildRole(context.Background(), createRoleReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.createRoleRequest.GetActorUserId())
	require.Equal(t, uint64(16), guildClient.createRoleRequest.GetPermissions())
	require.Equal(t, int64(4001), result.GetRole().GetId())
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

	getRoleReq := new(apiv1.GetGuildRoleRequest)
	getRoleReq.SetGuildId(3001)
	getRoleReq.SetRoleId(4001)
	resp, err := client.GetGuildRole(context.Background(), getRoleReq)
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

	listRolesReq := new(apiv1.ListGuildRolesRequest)
	listRolesReq.SetGuildId(3001)
	resp, err := client.ListGuildRoles(context.Background(), listRolesReq)
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

	updateRoleReq := new(apiv1.UpdateGuildRoleRequest)
	updateRoleReq.SetGuildId(3001)
	updateRoleReq.SetRoleId(4001)
	updateRoleReq.SetPermissions(32)
	_, err := client.UpdateGuildRole(context.Background(), updateRoleReq)
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

	deleteRoleReq := new(apiv1.DeleteGuildRoleRequest)
	deleteRoleReq.SetGuildId(3001)
	deleteRoleReq.SetRoleId(4001)
	resp, err := client.DeleteGuildRole(context.Background(), deleteRoleReq)
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

	pos1 := new(apiv1.GuildRolePosition)
	pos1.SetRoleId(4001)
	pos1.SetPosition(0)
	pos2 := new(apiv1.GuildRolePosition)
	pos2.SetRoleId(4002)
	pos2.SetPosition(1)
	reorderRolesReq := new(apiv1.ReorderGuildRolesRequest)
	reorderRolesReq.SetGuildId(3001)
	reorderRolesReq.SetPositions([]*apiv1.GuildRolePosition{pos1, pos2})
	resp, err := client.ReorderGuildRoles(context.Background(), reorderRolesReq)
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

	addMemberRoleReq := new(apiv1.AddGuildMemberRoleRequest)
	addMemberRoleReq.SetGuildId(3001)
	addMemberRoleReq.SetUserId(1002)
	addMemberRoleReq.SetRoleId(4001)
	resp, err := client.AddGuildMemberRole(context.Background(), addMemberRoleReq)
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

	removeMemberRoleReq := new(apiv1.RemoveGuildMemberRoleRequest)
	removeMemberRoleReq.SetGuildId(3001)
	removeMemberRoleReq.SetUserId(1002)
	removeMemberRoleReq.SetRoleId(4001)
	resp, err := client.RemoveGuildMemberRole(context.Background(), removeMemberRoleReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.removeMemberRoleReq.GetActorUserId())
	require.True(t, resp.GetOk())
}

func TestAddGuildRoleMembersUsesAuthenticatedActor(t *testing.T) {
	guildClient := &fakeGuildClient{
		addRoleMembersFn: func(*guildv1.AddGuildRoleMembersRequest) (*guildv1.AddGuildRoleMembersResponse, error) {
			resp := new(guildv1.AddGuildRoleMembersResponse)
			resp.SetOk(true)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	req := new(apiv1.AddGuildRoleMembersRequest)
	req.SetGuildId(3001)
	req.SetRoleId(4001)
	req.SetUserIds([]int64{1002, 1003})
	resp, err := client.AddGuildRoleMembers(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.addRoleMembersReq.GetActorUserId())
	require.Equal(t, int64(4001), guildClient.addRoleMembersReq.GetRoleId())
	require.Equal(t, []int64{1002, 1003}, guildClient.addRoleMembersReq.GetUserIds())
	require.True(t, resp.GetOk())
}

func TestRemoveGuildRoleMembersUsesAuthenticatedActor(t *testing.T) {
	guildClient := &fakeGuildClient{
		removeRoleMembersFn: func(*guildv1.RemoveGuildRoleMembersRequest) (*guildv1.RemoveGuildRoleMembersResponse, error) {
			resp := new(guildv1.RemoveGuildRoleMembersResponse)
			resp.SetOk(true)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	req := new(apiv1.RemoveGuildRoleMembersRequest)
	req.SetGuildId(3001)
	req.SetRoleId(4001)
	req.SetUserIds([]int64{1002, 1003})
	resp, err := client.RemoveGuildRoleMembers(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.removeRoleMembersReq.GetActorUserId())
	require.Equal(t, []int64{1002, 1003}, guildClient.removeRoleMembersReq.GetUserIds())
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

	listMemberRolesReq := new(apiv1.ListGuildMemberRolesRequest)
	listMemberRolesReq.SetGuildId(3001)
	listMemberRolesReq.SetUserId(1002)
	resp, err := client.ListGuildMemberRoles(context.Background(), listMemberRolesReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.listMemberRolesReq.GetActorUserId())
	require.Len(t, resp.GetRoles(), 1)
}
