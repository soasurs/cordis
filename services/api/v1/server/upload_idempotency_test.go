package server

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	"github.com/soasurs/cordis/pkg/rpcerror"
)

func TestCreateAvatarUploadForwardsIdempotencyKey(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	svcResp := new(userv1.CreateAvatarUploadResponse)
	svcResp.SetUploadId(7001)
	svcResp.SetPresignedUrl("https://upload.example/7001")
	userClient := &fakeUserClient{createAvatarUploadResponse: svcResp}
	client, closeServer := newUserHTTPClient(t, authenticatorClient, userClient, "access-token")
	defer closeServer()

	req := new(apiv1.CreateAvatarUploadRequest)
	req.SetExpectedSize(123)
	req.SetContentType("image/png")
	req.SetIdempotencyKey("avatar-intent-1")
	_, err := client.CreateAvatarUpload(context.Background(), req)
	require.NoError(t, err)
	require.True(t, userClient.createAvatarUploadRequest.HasIdempotencyKey())
	require.Equal(t, "avatar-intent-1", userClient.createAvatarUploadRequest.GetIdempotencyKey())
}

func TestCreateGuildIconUploadForwardsIdempotencyKey(t *testing.T) {
	svcResp := new(guildv1.CreateGuildIconUploadResponse)
	svcResp.SetUploadId(7001)
	svcResp.SetPresignedUrl("https://upload.example/7001")
	guildClient := &fakeGuildClient{createIconResponse: svcResp}
	client, closeServer := newGuildHTTPClient(t, guildClient)
	defer closeServer()

	req := new(apiv1.CreateGuildIconUploadRequest)
	req.SetGuildId(3001)
	req.SetExpectedSize(123)
	req.SetContentType("image/png")
	req.SetIdempotencyKey("icon-intent-1")
	_, err := client.CreateGuildIconUpload(context.Background(), req)
	require.NoError(t, err)
	require.True(t, guildClient.createIconRequest.HasIdempotencyKey())
	require.Equal(t, "icon-intent-1", guildClient.createIconRequest.GetIdempotencyKey())
}

func TestCreateAttachmentUploadForwardsIdempotencyKey(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	svcResp := new(messagev1.CreateAttachmentUploadResponse)
	svcResp.SetUploadId(7001)
	svcResp.SetPresignedUrl("https://upload.example/7001")
	messageClient := &fakeMessageClient{uploadResponse: svcResp}
	client, closeServer := newMessageHTTPClient(t, authenticatorClient, messageClient, "access-token")
	defer closeServer()

	req := new(apiv1.CreateAttachmentUploadRequest)
	req.SetChannelId(2001)
	req.SetExpectedSize(123)
	req.SetContentType("application/pdf")
	req.SetFilename("report.pdf")
	req.SetIdempotencyKey("attachment-intent-1")
	_, err := client.CreateAttachmentUpload(context.Background(), req)
	require.NoError(t, err)
	require.True(t, messageClient.uploadRequest.HasIdempotencyKey())
	require.Equal(t, "attachment-intent-1", messageClient.uploadRequest.GetIdempotencyKey())
}

func TestUploadRPCsMapIdempotencyKeyReuse(t *testing.T) {
	reused := func() error {
		return rpcerror.New(
			codes.InvalidArgument,
			rpcerror.MediaDomain,
			rpcerror.MediaIdempotencyKeyReused,
			"idempotency key was reused",
		)
	}

	t.Run("avatar", func(t *testing.T) {
		authenticatorClient := &fakeAuthenticatorClient{
			verifyResponse: verifyAccessTokenResponse(1001),
		}
		userClient := &fakeUserClient{createAvatarUploadError: reused()}
		client, closeServer := newUserHTTPClient(t, authenticatorClient, userClient, "access-token")
		defer closeServer()

		req := new(apiv1.CreateAvatarUploadRequest)
		req.SetExpectedSize(123)
		req.SetContentType("image/png")
		req.SetIdempotencyKey("avatar-intent-1")
		_, err := client.CreateAvatarUpload(context.Background(), req)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		require.Equal(t, apierror.CodeIdempotencyKeyReused, publicErrorInfo(t, err).GetCode())
	})

	t.Run("guild icon", func(t *testing.T) {
		guildClient := &fakeGuildClient{createIconError: reused()}
		client, closeServer := newGuildHTTPClient(t, guildClient)
		defer closeServer()

		req := new(apiv1.CreateGuildIconUploadRequest)
		req.SetGuildId(3001)
		req.SetExpectedSize(123)
		req.SetContentType("image/png")
		req.SetIdempotencyKey("icon-intent-1")
		_, err := client.CreateGuildIconUpload(context.Background(), req)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		require.Equal(t, apierror.CodeIdempotencyKeyReused, publicErrorInfo(t, err).GetCode())
	})

	t.Run("attachment", func(t *testing.T) {
		authenticatorClient := &fakeAuthenticatorClient{
			verifyResponse: verifyAccessTokenResponse(1001),
		}
		messageClient := &fakeMessageClient{uploadError: reused()}
		client, closeServer := newMessageHTTPClient(t, authenticatorClient, messageClient, "access-token")
		defer closeServer()

		req := new(apiv1.CreateAttachmentUploadRequest)
		req.SetChannelId(2001)
		req.SetExpectedSize(123)
		req.SetContentType("application/pdf")
		req.SetFilename("report.pdf")
		req.SetIdempotencyKey("attachment-intent-1")
		_, err := client.CreateAttachmentUpload(context.Background(), req)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		require.Equal(t, apierror.CodeIdempotencyKeyReused, publicErrorInfo(t, err).GetCode())
	})
}
