package server

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
)

func TestSearchGuildMentionUsersForwardsAuthenticatedActorAndNickname(t *testing.T) {
	user := new(guildv1.GuildMentionUser)
	user.SetUserId(1002)
	user.SetUsername("alice")
	user.SetNickname("Alice in Guild")
	user.SetName("Alice")
	user.SetAvatarAssetId(6001)
	searchResp := new(guildv1.SearchGuildMentionUsersResponse)
	searchResp.SetUsers([]*guildv1.GuildMentionUser{user})
	guildClient := &fakeGuildClient{searchUsersResp: searchResp}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	request := new(apiv1.SearchGuildMentionUsersRequest)
	request.SetGuildId(3001)
	request.SetChannelId(5001)
	request.SetQuery("ali")
	request.SetLimit(7)
	response, err := client.SearchGuildMentionUsers(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, int64(3001), guildClient.searchUsersReq.GetGuildId())
	require.Equal(t, int64(1001), guildClient.searchUsersReq.GetActorUserId())
	require.Equal(t, int64(5001), guildClient.searchUsersReq.GetChannelId())
	require.Equal(t, "ali", guildClient.searchUsersReq.GetQuery())
	require.Equal(t, int32(7), guildClient.searchUsersReq.GetLimit())
	require.Equal(t, "Alice in Guild", response.GetUsers()[0].GetNickname())
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

	addMemberReq := new(apiv1.AddGuildMemberRequest)
	addMemberReq.SetGuildId(3001)
	addMemberReq.SetUserId(1002)
	addMemberResp, err := client.AddGuildMember(context.Background(), addMemberReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.addMemberRequest.GetActorUserId())
	require.Equal(t, int64(1002), guildClient.addMemberRequest.GetUserId())
	require.Equal(t, int64(1001), addMemberResp.GetMember().GetProfile().GetUserId())

	updateCurrentMemberReq := new(apiv1.UpdateCurrentGuildMemberRequest)
	updateCurrentMemberReq.SetGuildId(3001)
	updateCurrentMemberReq.SetNickname("member")
	updateMemberResp, err := client.UpdateCurrentGuildMember(context.Background(), updateCurrentMemberReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.updateMemberRequest.GetActorUserId())
	require.Equal(t, int64(1001), updateMemberResp.GetMember().GetProfile().GetUserId())

	leaveReq := new(apiv1.LeaveGuildRequest)
	leaveReq.SetGuildId(3001)
	_, err = client.LeaveGuild(context.Background(), leaveReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.leaveRequest.GetUserId())

	transferReq := new(apiv1.TransferGuildOwnershipRequest)
	transferReq.SetGuildId(3001)
	transferReq.SetNewOwnerId(1002)
	_, err = client.TransferGuildOwnership(context.Background(), transferReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.transferRequest.GetActorUserId())
	require.Equal(t, int64(1002), guildClient.transferRequest.GetNewOwnerId())
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

	getMemberReq := new(apiv1.GetGuildMemberRequest)
	getMemberReq.SetGuildId(3001)
	getMemberReq.SetUserId(1002)
	resp, err := client.GetGuildMember(context.Background(), getMemberReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.getMemberReq.GetActorUserId())
	require.Equal(t, int64(1002), guildClient.getMemberReq.GetUserId())
	require.Equal(t, int64(1001), resp.GetMember().GetUserId()) // returns internalGuildMember which has UserId=1001
	require.Equal(t, "display name", resp.GetMember().GetProfile().GetName())
}

func TestListGuildMembersMapsRequestAndResponse(t *testing.T) {
	member := internalGuildMember()
	guildClient := &fakeGuildClient{
		listMembersFn: func(*guildv1.ListGuildMembersRequest) (*guildv1.ListGuildMembersResponse, error) {
			resp := new(guildv1.ListGuildMembersResponse)
			resp.SetMembers([]*guildv1.GuildMember{member})
			resp.SetNextCursor("cursor-token")
			return resp, nil
		},
	}
	profilesResp := new(userv1.BatchGetUserProfilesResponse)
	profilesResp.SetProfiles([]*userv1.UserProfile{internalUserProfile()})
	userClient := &fakeUserClient{batchGetUserProfilesResponse: profilesResp}
	client, closeServer := newGuildHTTPClientWithUser(t, guildClient, userClient)
	defer closeServer()

	listMembersReq := new(apiv1.ListGuildMembersRequest)
	listMembersReq.SetGuildId(3001)
	listMembersReq.SetCursor("cursor-token")
	listMembersReq.SetLimit(50)
	resp, err := client.ListGuildMembers(context.Background(), listMembersReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.listMembersReq.GetActorUserId())
	require.Equal(t, "cursor-token", guildClient.listMembersReq.GetCursor())
	require.Equal(t, int32(50), guildClient.listMembersReq.GetLimit())
	require.Equal(t, []int64{1001}, userClient.batchGetUserProfilesRequest.GetUserIds())
	require.Len(t, resp.GetMembers(), 1)
	require.Equal(t, "display name", resp.GetMembers()[0].GetProfile().GetName())
	require.Equal(t, int64(6001), resp.GetMembers()[0].GetProfile().GetAvatarAssetId())
	require.Equal(t, "cursor-token", resp.GetNextCursor())
}

func TestListGuildMembersRejectsMissingProfile(t *testing.T) {
	guildClient := &fakeGuildClient{
		listMembersFn: func(*guildv1.ListGuildMembersRequest) (*guildv1.ListGuildMembersResponse, error) {
			resp := new(guildv1.ListGuildMembersResponse)
			resp.SetMembers([]*guildv1.GuildMember{internalGuildMember()})
			return resp, nil
		},
	}
	profilesResp := new(userv1.BatchGetUserProfilesResponse)
	userClient := &fakeUserClient{batchGetUserProfilesResponse: profilesResp}
	client, closeServer := newGuildHTTPClientWithUser(t, guildClient, userClient)
	defer closeServer()

	req := new(apiv1.ListGuildMembersRequest)
	req.SetGuildId(3001)
	_, err := client.ListGuildMembers(context.Background(), req)
	require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
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

	kickReq := new(apiv1.KickGuildMemberRequest)
	kickReq.SetGuildId(3001)
	kickReq.SetUserId(1002)
	resp, err := client.KickGuildMember(context.Background(), kickReq)
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

	banReq := new(apiv1.BanGuildMemberRequest)
	banReq.SetGuildId(3001)
	banReq.SetUserId(1002)
	banReq.SetReason("spam")
	resp, err := client.BanGuildMember(context.Background(), banReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.banReq.GetActorUserId())
	require.Equal(t, int64(1002), guildClient.banReq.GetUserId())
	require.Equal(t, "spam", guildClient.banReq.GetReason())
	require.Equal(t, int64(1002), resp.GetBan().GetUserId())
	require.Equal(t, int64(1002), resp.GetBan().GetProfile().GetUserId())
	require.Equal(t, int64(1001), resp.GetBan().GetActorProfile().GetUserId())
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

	unbanReq := new(apiv1.UnbanGuildMemberRequest)
	unbanReq.SetGuildId(3001)
	unbanReq.SetUserId(1002)
	resp, err := client.UnbanGuildMember(context.Background(), unbanReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.unbanReq.GetActorUserId())
	require.True(t, resp.GetOk())
}

func TestListGuildBansMapsRequestAndResponse(t *testing.T) {
	ban := new(guildv1.GuildBan)
	ban.SetGuildId(3001)
	ban.SetUserId(1002)
	ban.SetActorUserId(1001)
	ban.SetCreatedAt(4001)
	guildClient := &fakeGuildClient{
		listBansFn: func(*guildv1.ListGuildBansRequest) (*guildv1.ListGuildBansResponse, error) {
			resp := new(guildv1.ListGuildBansResponse)
			resp.SetBans([]*guildv1.GuildBan{ban})
			resp.SetNextCursor("cursor-token")
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	listBansReq := new(apiv1.ListGuildBansRequest)
	listBansReq.SetGuildId(3001)
	listBansReq.SetCursor("cursor-token")
	listBansReq.SetLimit(20)
	resp, err := client.ListGuildBans(context.Background(), listBansReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.listBansReq.GetActorUserId())
	require.Equal(t, "cursor-token", guildClient.listBansReq.GetCursor())
	require.Len(t, resp.GetBans(), 1)
	require.Equal(t, int64(1002), resp.GetBans()[0].GetProfile().GetUserId())
	require.Equal(t, int64(1001), resp.GetBans()[0].GetActorProfile().GetUserId())
	require.Equal(t, "cursor-token", resp.GetNextCursor())
}

func TestListGuildRoleMembersMapsRequestProfilesAndCursor(t *testing.T) {
	guildClient := &fakeGuildClient{
		listRoleMembersFn: func(*guildv1.ListGuildRoleMembersRequest) (*guildv1.ListGuildRoleMembersResponse, error) {
			resp := new(guildv1.ListGuildRoleMembersResponse)
			resp.SetMembers([]*guildv1.GuildMember{internalGuildMember()})
			resp.SetNextCursor("cursor-token")
			return resp, nil
		},
	}
	profilesResp := new(userv1.BatchGetUserProfilesResponse)
	profilesResp.SetProfiles([]*userv1.UserProfile{internalUserProfile()})
	userClient := &fakeUserClient{batchGetUserProfilesResponse: profilesResp}
	client, closeServer := newGuildHTTPClientWithUser(t, guildClient, userClient)
	defer closeServer()

	req := new(apiv1.ListGuildRoleMembersRequest)
	req.SetGuildId(3001)
	req.SetRoleId(4001)
	req.SetCursor("cursor-token")
	req.SetLimit(25)
	resp, err := client.ListGuildRoleMembers(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.listRoleMembersReq.GetActorUserId())
	require.Equal(t, int64(4001), guildClient.listRoleMembersReq.GetRoleId())
	require.Equal(t, "cursor-token", guildClient.listRoleMembersReq.GetCursor())
	require.Equal(t, int32(25), guildClient.listRoleMembersReq.GetLimit())
	require.Len(t, resp.GetMembers(), 1)
	require.Equal(t, "display name", resp.GetMembers()[0].GetProfile().GetName())
	require.Equal(t, "cursor-token", resp.GetNextCursor())
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

	permissionsReq := new(apiv1.GetGuildMemberPermissionsRequest)
	permissionsReq.SetGuildId(3001)
	permissionsReq.SetUserId(1002)
	resp, err := client.GetGuildMemberPermissions(context.Background(), permissionsReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.permissionsReq.GetActorUserId())
	require.Equal(t, int64(1002), guildClient.permissionsReq.GetUserId())
	require.Equal(t, uint64(42), resp.GetPermissions())
}
