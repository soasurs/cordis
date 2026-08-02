package server

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	"github.com/soasurs/cordis/pkg/rpcerror"
)

func TestCreateGuildUsesAuthenticatedOwner(t *testing.T) {
	internal := internalGuild()
	createResp := new(guildv1.CreateGuildResponse)
	createResp.SetGuild(internal)
	guildClient := &fakeGuildClient{createResponse: createResp}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	createReq := new(apiv1.CreateGuildRequest)
	createReq.SetName("Cordis")
	resp, err := client.CreateGuild(context.Background(), createReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.createRequest.GetOwnerId())
	require.Equal(t, "Cordis", guildClient.createRequest.GetName())
	require.Equal(t, int64(3001), resp.GetGuild().GetId())
	require.Equal(t, "Community description", resp.GetGuild().GetDescription())
}

func TestUpdateGuildUsesAuthenticatedActorAndFieldPresence(t *testing.T) {
	updateResp := new(guildv1.UpdateGuildResponse)
	updateResp.SetGuild(internalGuild())
	guildClient := &fakeGuildClient{updateResponse: updateResp}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	updateReq := new(apiv1.UpdateGuildRequest)
	updateReq.SetGuildId(3001)
	updateReq.SetName("")
	updateReq.SetDescription("Community description")
	_, err := client.UpdateGuild(context.Background(), updateReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.updateRequest.GetActorUserId())
	require.True(t, guildClient.updateRequest.HasName())
	require.Empty(t, guildClient.updateRequest.GetName())
	require.True(t, guildClient.updateRequest.HasDescription())
	require.Equal(t, "Community description", guildClient.updateRequest.GetDescription())
}

func TestCreateGuildIconUploadUsesAuthenticatedActor(t *testing.T) {
	svcResp := new(guildv1.CreateGuildIconUploadResponse)
	svcResp.SetUploadId(7001)
	svcResp.SetPresignedUrl("https://upload.example/7001")
	svcResp.SetExpiresAt(9001)
	svcResp.SetRequestHeaders(map[string]string{"Content-Type": "image/png"})
	svcResp.SetStatus(mediav1.AssetStatus_ASSET_STATUS_READY)
	svcResp.SetIdempotentReplay(true)
	guildClient := &fakeGuildClient{createIconResponse: svcResp}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	req := new(apiv1.CreateGuildIconUploadRequest)
	req.SetGuildId(3001)
	req.SetExpectedSize(123)
	req.SetContentType("image/png")
	resp, err := client.CreateGuildIconUpload(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.createIconRequest.GetActorUserId())
	require.Equal(t, int64(3001), guildClient.createIconRequest.GetGuildId())
	require.Equal(t, int64(123), guildClient.createIconRequest.GetExpectedSize())
	require.Equal(t, int64(7001), resp.GetUploadId())
	require.Equal(t, map[string]string{"Content-Type": "image/png"}, resp.GetRequestHeaders())
	require.Equal(t, apiv1.UploadStatus_UPLOAD_STATUS_READY, resp.GetStatus())
	require.True(t, resp.GetIdempotentReplay())
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

	getGuildReq := new(apiv1.GetGuildRequest)
	getGuildReq.SetGuildId(3001)
	resp, err := client.GetGuild(context.Background(), getGuildReq)
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
			resp.SetNextCursor("cursor-token")
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	listGuildsReq := new(apiv1.ListGuildsRequest)
	listGuildsReq.SetCursor("cursor-token")
	listGuildsReq.SetLimit(25)
	resp, err := client.ListGuilds(context.Background(), listGuildsReq)
	require.NoError(t, err)
	require.Equal(t, int64(1001), guildClient.listGuildsReq.GetUserId())
	require.Equal(t, "cursor-token", guildClient.listGuildsReq.GetCursor())
	require.Equal(t, int32(25), guildClient.listGuildsReq.GetLimit())
	require.Len(t, resp.GetGuilds(), 1)
	require.Equal(t, "cursor-token", resp.GetNextCursor())
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

	deleteReq := new(apiv1.DeleteGuildRequest)
	deleteReq.SetGuildId(3001)
	resp, err := client.DeleteGuild(context.Background(), deleteReq)
	require.NoError(t, err)
	require.Equal(t, int64(3001), guildClient.deleteReq.GetGuildId())
	require.Equal(t, int64(1001), guildClient.deleteReq.GetActorUserId())
	require.True(t, resp.GetOk())
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
				getGuildReq := new(apiv1.GetGuildRequest)
				getGuildReq.SetGuildId(3001)
				_, err := client.GetGuild(context.Background(), getGuildReq)
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
				updateRoleReq := new(apiv1.UpdateGuildRoleRequest)
				updateRoleReq.SetGuildId(3001)
				updateRoleReq.SetRoleId(4001)
				updateRoleReq.SetName("test")
				_, err := client.UpdateGuildRole(context.Background(), updateRoleReq)
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
