package server

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	"github.com/soasurs/cordis/pkg/rpcerror"
)

func TestCreateGuildForwardsIdempotencyKey(t *testing.T) {
	guildClient := &fakeGuildClient{}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	req := new(apiv1.CreateGuildRequest)
	req.SetName("Cordis")
	req.SetIdempotencyKey("guild-intent-1")
	_, err := client.CreateGuild(context.Background(), req)
	require.NoError(t, err)
	require.True(t, guildClient.createRequest.HasIdempotencyKey())
	require.Equal(t, "guild-intent-1", guildClient.createRequest.GetIdempotencyKey())
}

func TestCreateGuildRoleForwardsIdempotencyKey(t *testing.T) {
	guildClient := &fakeGuildClient{
		createRoleFn: func(*guildv1.CreateGuildRoleRequest) (*guildv1.CreateGuildRoleResponse, error) {
			resp := new(guildv1.CreateGuildRoleResponse)
			resp.SetRole(internalGuildRole())
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	req := new(apiv1.CreateGuildRoleRequest)
	req.SetGuildId(3001)
	req.SetName("Moderator")
	req.SetIdempotencyKey("role-intent-1")
	_, err := client.CreateGuildRole(context.Background(), req)
	require.NoError(t, err)
	require.True(t, guildClient.createRoleRequest.HasIdempotencyKey())
	require.Equal(t, "role-intent-1", guildClient.createRoleRequest.GetIdempotencyKey())
}

func TestCreateGuildChannelForwardsIdempotencyKey(t *testing.T) {
	guildClient := &fakeGuildClient{
		createChannelFn: func(*guildv1.CreateGuildChannelRequest) (*guildv1.CreateGuildChannelResponse, error) {
			resp := new(guildv1.CreateGuildChannelResponse)
			resp.SetChannel(internalGuildChannel())
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	req := new(apiv1.CreateGuildChannelRequest)
	req.SetGuildId(3001)
	req.SetName("general")
	req.SetIdempotencyKey("channel-intent-1")
	_, err := client.CreateGuildChannel(context.Background(), req)
	require.NoError(t, err)
	require.True(t, guildClient.createChannelRequest.HasIdempotencyKey())
	require.Equal(t, "channel-intent-1", guildClient.createChannelRequest.GetIdempotencyKey())
}

func TestCreateGuildInviteForwardsIdempotencyKey(t *testing.T) {
	guildClient := &fakeGuildClient{
		createInviteFn: func(*guildv1.CreateGuildInviteRequest) (*guildv1.CreateGuildInviteResponse, error) {
			resp := new(guildv1.CreateGuildInviteResponse)
			resp.SetInvite(internalGuildInvite())
			return resp, nil
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	req := new(apiv1.CreateGuildInviteRequest)
	req.SetGuildId(3001)
	req.SetMaxUses(5)
	req.SetIdempotencyKey("invite-intent-1")
	_, err := client.CreateGuildInvite(context.Background(), req)
	require.NoError(t, err)
	require.True(t, guildClient.createInviteReq.HasIdempotencyKey())
	require.Equal(t, "invite-intent-1", guildClient.createInviteReq.GetIdempotencyKey())
}

func TestCreateGuildMapsIdempotencyKeyReuse(t *testing.T) {
	guildClient := &fakeGuildClient{
		createFn: func(*guildv1.CreateGuildRequest) (*guildv1.CreateGuildResponse, error) {
			return nil, rpcerror.New(
				codes.InvalidArgument,
				rpcerror.GuildDomain,
				rpcerror.GuildIdempotencyKeyReused,
				"idempotency key was reused",
			)
		},
	}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	req := new(apiv1.CreateGuildRequest)
	req.SetName("Cordis")
	req.SetIdempotencyKey("guild-intent-1")
	_, err := client.CreateGuild(context.Background(), req)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	require.Equal(t, apierror.CodeIdempotencyKeyReused, publicErrorInfo(t, err).GetCode())
}
