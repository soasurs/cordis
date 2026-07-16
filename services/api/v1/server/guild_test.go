package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

type fakeGuildClient struct {
	guildv1.GuildServiceClient
	createRequest        *guildv1.CreateGuildRequest
	updateRequest        *guildv1.UpdateGuildRequest
	addMemberRequest     *guildv1.AddGuildMemberRequest
	updateMemberRequest  *guildv1.UpdateGuildMemberRequest
	leaveRequest         *guildv1.LeaveGuildRequest
	transferRequest      *guildv1.TransferGuildOwnershipRequest
	createResponse       *guildv1.CreateGuildResponse
	updateResponse       *guildv1.UpdateGuildResponse
	addMemberResponse    *guildv1.AddGuildMemberResponse
	updateMemberResponse *guildv1.UpdateGuildMemberResponse
	leaveResponse        *guildv1.LeaveGuildResponse
	transferResponse     *guildv1.TransferGuildOwnershipResponse
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
