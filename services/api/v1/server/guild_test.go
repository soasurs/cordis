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
	createRequest  *guildv1.CreateGuildRequest
	updateRequest  *guildv1.UpdateGuildRequest
	createResponse *guildv1.CreateGuildResponse
	updateResponse *guildv1.UpdateGuildResponse
}

func (f *fakeGuildClient) CreateGuild(_ context.Context, req *guildv1.CreateGuildRequest, _ ...grpc.CallOption) (*guildv1.CreateGuildResponse, error) {
	f.createRequest = req
	return f.createResponse, nil
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
