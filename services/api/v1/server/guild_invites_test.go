package server

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
)

func (f *fakeGuildClient) CreateGuildInvite(_ context.Context, req *guildv1.CreateGuildInviteRequest, _ ...grpc.CallOption) (*guildv1.CreateGuildInviteResponse, error) {
	f.createInviteReq = req
	if f.createInviteFn != nil {
		return f.createInviteFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) GetGuildInvite(_ context.Context, req *guildv1.GetGuildInviteRequest, _ ...grpc.CallOption) (*guildv1.GetGuildInviteResponse, error) {
	f.getInviteReq = req
	if f.getInviteFn != nil {
		return f.getInviteFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) ListGuildInvites(_ context.Context, req *guildv1.ListGuildInvitesRequest, _ ...grpc.CallOption) (*guildv1.ListGuildInvitesResponse, error) {
	f.listInvitesReq = req
	if f.listInvitesFn != nil {
		return f.listInvitesFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) DeleteGuildInvite(_ context.Context, req *guildv1.DeleteGuildInviteRequest, _ ...grpc.CallOption) (*guildv1.DeleteGuildInviteResponse, error) {
	f.deleteInviteReq = req
	if f.deleteInviteFn != nil {
		return f.deleteInviteFn(req)
	}
	return nil, nil
}

func (f *fakeGuildClient) JoinGuildByInvite(_ context.Context, req *guildv1.JoinGuildByInviteRequest, _ ...grpc.CallOption) (*guildv1.JoinGuildByInviteResponse, error) {
	f.joinInviteReq = req
	if f.joinInviteFn != nil {
		return f.joinInviteFn(req)
	}
	return nil, nil
}

func internalGuildInvite() *guildv1.GuildInvite {
	invite := new(guildv1.GuildInvite)
	invite.SetId(5001)
	invite.SetCode("invite-code")
	invite.SetGuildId(3001)
	invite.SetCreatorUserId(1001)
	invite.SetMaxUses(5)
	invite.SetUses(1)
	invite.SetExpiresAt(4002)
	invite.SetCreatedAt(4001)
	return invite
}

func TestCreateGuildInviteUsesAuthenticatedActor(t *testing.T) {
	guildClient := &fakeGuildClient{
		createInviteFn: func(*guildv1.CreateGuildInviteRequest) (*guildv1.CreateGuildInviteResponse, error) {
			resp := new(guildv1.CreateGuildInviteResponse)
			resp.SetInvite(internalGuildInvite())
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.CreateGuildInvite(context.Background(), &apiv1.CreateGuildInviteRequest{
		GuildId: new(int64(3001)), MaxUses: new(int32(5)), ExpiresInMs: new(int64(60_000)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.createInviteReq.GetActorUserId())
	require.Equal(t, int64(3001), guildClient.createInviteReq.GetGuildId())
	require.Equal(t, int32(5), guildClient.createInviteReq.GetMaxUses())
	require.Equal(t, int64(60_000), guildClient.createInviteReq.GetExpiresInMs())
	require.Equal(t, "invite-code", resp.GetInvite().GetCode())
	require.Equal(t, int64(5001), resp.GetInvite().GetId())
}

func TestGetGuildInviteMapsPreview(t *testing.T) {
	guildClient := &fakeGuildClient{
		getInviteFn: func(*guildv1.GetGuildInviteRequest) (*guildv1.GetGuildInviteResponse, error) {
			preview := new(guildv1.GuildInvitePreview)
			preview.SetCode("invite-code")
			preview.SetGuildId(3001)
			preview.SetGuildName("Cordis")
			preview.SetGuildIconUri("icon://guild")
			preview.SetMemberCount(42)
			preview.SetExpiresAt(4002)
			resp := new(guildv1.GetGuildInviteResponse)
			resp.SetPreview(preview)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.GetGuildInvite(context.Background(), &apiv1.GetGuildInviteRequest{
		Code: new("invite-code"),
	})
	require.NoError(t, err)
	require.Equal(t, "invite-code", guildClient.getInviteReq.GetCode())
	require.Equal(t, "Cordis", resp.GetPreview().GetGuildName())
	require.Equal(t, int64(42), resp.GetPreview().GetMemberCount())
}

func TestGetGuildInviteMapsNotFound(t *testing.T) {
	guildClient := &fakeGuildClient{
		getInviteFn: func(*guildv1.GetGuildInviteRequest) (*guildv1.GetGuildInviteResponse, error) {
			return nil, rpcerror.New(codes.NotFound, rpcerror.GuildDomain, rpcerror.GuildInviteNotFound, "guild invite not found")
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	_, err := client.GetGuildInvite(context.Background(), &apiv1.GetGuildInviteRequest{Code: new("missing")})
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

func TestListGuildInvitesMapsRequestAndResponse(t *testing.T) {
	guildClient := &fakeGuildClient{
		listInvitesFn: func(*guildv1.ListGuildInvitesRequest) (*guildv1.ListGuildInvitesResponse, error) {
			resp := new(guildv1.ListGuildInvitesResponse)
			resp.SetInvites([]*guildv1.GuildInvite{internalGuildInvite()})
			resp.SetBeforeId(5001)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.ListGuildInvites(context.Background(), &apiv1.ListGuildInvitesRequest{
		GuildId: new(int64(3001)), BeforeId: new(int64(6000)), Limit: new(int32(20)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.listInvitesReq.GetActorUserId())
	require.Equal(t, int64(6000), guildClient.listInvitesReq.GetBeforeId())
	require.Equal(t, int32(20), guildClient.listInvitesReq.GetLimit())
	require.Len(t, resp.GetInvites(), 1)
	require.Equal(t, int64(5001), resp.GetBeforeId())
}

func TestDeleteGuildInviteUsesAuthenticatedActor(t *testing.T) {
	guildClient := &fakeGuildClient{
		deleteInviteFn: func(*guildv1.DeleteGuildInviteRequest) (*guildv1.DeleteGuildInviteResponse, error) {
			resp := new(guildv1.DeleteGuildInviteResponse)
			resp.SetOk(true)
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.DeleteGuildInvite(context.Background(), &apiv1.DeleteGuildInviteRequest{
		Code: new("invite-code"),
	})
	require.NoError(t, err)
	require.Equal(t, "invite-code", guildClient.deleteInviteReq.GetCode())
	require.Equal(t, int64(1001), guildClient.deleteInviteReq.GetActorUserId())
	require.True(t, resp.GetOk())
}

func TestJoinGuildByInviteUsesAuthenticatedUser(t *testing.T) {
	guildClient := &fakeGuildClient{
		joinInviteFn: func(*guildv1.JoinGuildByInviteRequest) (*guildv1.JoinGuildByInviteResponse, error) {
			resp := new(guildv1.JoinGuildByInviteResponse)
			resp.SetGuild(internalGuild())
			resp.SetMember(internalGuildMember())
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	resp, err := client.JoinGuildByInvite(context.Background(), &apiv1.JoinGuildByInviteRequest{
		Code: new("invite-code"),
	})
	require.NoError(t, err)
	require.Equal(t, "invite-code", guildClient.joinInviteReq.GetCode())
	require.Equal(t, int64(1001), guildClient.joinInviteReq.GetUserId())
	require.Equal(t, int64(3001), resp.GetGuild().GetId())
	require.NotNil(t, resp.GetMember())
}
