package server

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	"github.com/soasurs/cordis/pkg/rpcerror"
)

func TestCreateMessageUsesAuthenticatedAuthor(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	messageClient := &fakeMessageClient{
		createResponse: createMessageResponse(internalMessage()),
	}
	client, closeServer := newMessageHTTPClient(t, authenticatorClient, messageClient, "access-token")
	defer closeServer()

	attachment := new(apiv1.Attachment)
	attachment.SetAssetId(101)
	attachment.SetFilename("a.png")
	attachment.SetSize(10)
	attachment.SetContentType("image/png")
	attachment.SetWidth(100)
	attachment.SetHeight(200)

	req := new(apiv1.CreateMessageRequest)
	req.SetChannelId(2001)
	req.SetContent("hello")
	req.SetType(apiv1.MessageType_MESSAGE_TYPE_REPLY)
	req.SetFlags(int32(apiv1.MessageFlag_MESSAGE_FLAG_SUPPRESS_NOTIFICATIONS))
	req.SetReferencedMessageId(3000)
	req.SetReferencedChannelId(2001)
	req.SetAttachments([]*apiv1.Attachment{attachment})
	req.SetIdempotencyKey("message-intent-1")

	resp, err := client.CreateMessage(context.Background(), req)
	require.NoError(t, err)

	require.Equal(t, int64(1001), messageClient.createRequest.GetAuthorId())
	require.Equal(t, int64(2001), messageClient.createRequest.GetChannelId())
	require.Equal(t, messagev1.MessageType_MESSAGE_TYPE_REPLY, messageClient.createRequest.GetType())
	require.Equal(t, int32(messagev1.MessageFlag_MESSAGE_FLAG_SUPPRESS_NOTIFICATIONS), messageClient.createRequest.GetFlags())
	require.Equal(t, int64(3000), messageClient.createRequest.GetReferencedMessageId())
	require.Equal(t, int64(101), messageClient.createRequest.GetAttachments()[0].GetAssetId())
	require.True(t, messageClient.createRequest.HasIdempotencyKey())
	require.Equal(t, "message-intent-1", messageClient.createRequest.GetIdempotencyKey())
	require.Equal(t, int64(4001), resp.GetMessage().GetId())
	require.Equal(t, int64(2), resp.GetMessage().GetRevision())
	require.Equal(t, int64(1001), resp.GetMessage().GetAuthor().GetUserId())
	require.Equal(t, "https://download.example/101", resp.GetMessage().GetAttachments()[0].GetUrl())
	require.Equal(t, int64(9001), resp.GetMessage().GetAttachments()[0].GetUrlExpiresAt())
}

func TestCreateMessageMapsIdempotencyKeyReuse(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	messageClient := &fakeMessageClient{
		createError: rpcerror.New(
			codes.InvalidArgument,
			rpcerror.MessageDomain,
			rpcerror.MessageIdempotencyKeyReused,
			"idempotency key was reused",
		),
	}
	client, closeServer := newMessageHTTPClient(t, authenticatorClient, messageClient, "access-token")
	defer closeServer()

	req := new(apiv1.CreateMessageRequest)
	req.SetChannelId(2001)
	req.SetContent("hello")
	req.SetIdempotencyKey("message-intent-1")

	_, err := client.CreateMessage(context.Background(), req)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	require.Equal(t, apierror.CodeIdempotencyKeyReused, publicErrorInfo(t, err).GetCode())
}

func TestCreateAttachmentUploadForwardsRequestHeaders(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	svcResp := new(messagev1.CreateAttachmentUploadResponse)
	svcResp.SetUploadId(7001)
	svcResp.SetPresignedUrl("https://upload.example/7001")
	svcResp.SetExpiresAt(9001)
	svcResp.SetRequestHeaders(map[string]string{
		"Content-Length": "123",
		"Content-Type":   "application/pdf",
	})
	svcResp.SetStatus(mediav1.AssetStatus_ASSET_STATUS_CREATED)
	svcResp.SetIdempotentReplay(false)
	messageClient := &fakeMessageClient{uploadResponse: svcResp}
	client, closeServer := newMessageHTTPClient(t, authenticatorClient, messageClient, "access-token")
	defer closeServer()

	req := new(apiv1.CreateAttachmentUploadRequest)
	req.SetChannelId(2001)
	req.SetExpectedSize(123)
	req.SetContentType("application/pdf")
	req.SetFilename("report.pdf")
	resp, err := client.CreateAttachmentUpload(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), messageClient.uploadRequest.GetActorUserId())
	require.Equal(t, int64(2001), messageClient.uploadRequest.GetChannelId())
	require.Equal(t, int64(123), messageClient.uploadRequest.GetExpectedSize())
	require.Equal(t, "report.pdf", messageClient.uploadRequest.GetFilename())
	require.Equal(t, svcResp.GetRequestHeaders(), resp.GetRequestHeaders())
	require.Equal(t, apiv1.UploadStatus_UPLOAD_STATUS_CREATED, resp.GetStatus())
	require.False(t, resp.GetIdempotentReplay())
}

func TestUpdateMessagePreservesFieldPresence(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	messageClient := &fakeMessageClient{
		updateResponse: updateMessageResponse(internalMessage()),
	}
	client, closeServer := newMessageHTTPClient(t, authenticatorClient, messageClient, "access-token")
	defer closeServer()

	req := new(apiv1.UpdateMessageRequest)
	req.SetMessageId(4001)
	req.SetContent("")
	req.SetAttachments(new(apiv1.AttachmentList))
	resp, err := client.UpdateMessage(context.Background(), req)
	require.NoError(t, err)

	require.Equal(t, int64(1001), messageClient.updateRequest.GetActorUserId())
	require.True(t, messageClient.updateRequest.HasContent())
	require.Equal(t, "", messageClient.updateRequest.GetContent())
	require.False(t, messageClient.updateRequest.HasFlags())
	require.True(t, messageClient.updateRequest.HasAttachments())
	require.Empty(t, messageClient.updateRequest.GetAttachments().GetAttachments())
	require.Equal(t, int64(4001), resp.GetMessage().GetId())
}

func TestDeleteMessageUsesAuthenticatedActor(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	svcResp := new(messagev1.DeleteMessageResponse)
	svcResp.SetOk(true)
	messageClient := &fakeMessageClient{deleteResponse: svcResp}
	client, closeServer := newMessageHTTPClient(t, authenticatorClient, messageClient, "access-token")
	defer closeServer()

	req := new(apiv1.DeleteMessageRequest)
	req.SetMessageId(4001)
	resp, err := client.DeleteMessage(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(4001), messageClient.deleteRequest.GetMessageId())
	require.Equal(t, int64(1001), messageClient.deleteRequest.GetActorUserId())
	require.True(t, resp.GetOk())
}

func TestGetMessageRequiresAccessToken(t *testing.T) {
	client, closeServer := newMessageHTTPClient(t, &fakeAuthenticatorClient{}, &fakeMessageClient{}, "")
	defer closeServer()

	req := new(apiv1.GetMessageRequest)
	req.SetMessageId(4001)
	_, err := client.GetMessage(context.Background(), req)
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

func TestGetMessageUsesAuthenticatedUser(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{verifyResponse: verifyAccessTokenResponse(1001)}
	messageClient := &fakeMessageClient{getResponse: createGetMessageResponse(internalMessage())}
	client, closeServer := newMessageHTTPClient(t, authenticatorClient, messageClient, "access-token")
	defer closeServer()

	req := new(apiv1.GetMessageRequest)
	req.SetMessageId(4001)
	_, err := client.GetMessage(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), messageClient.getRequest.GetUserId())
}

func TestUpdateMessageMapsPermissionDenied(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	messageClient := &fakeMessageClient{
		updateError: rpcerror.New(
			codes.PermissionDenied,
			rpcerror.MessageDomain,
			rpcerror.MessagePermissionDenied,
			"permission denied",
		),
	}
	client, closeServer := newMessageHTTPClient(t, authenticatorClient, messageClient, "access-token")
	defer closeServer()

	req := new(apiv1.UpdateMessageRequest)
	req.SetMessageId(4001)
	req.SetContent("updated")
	_, err := client.UpdateMessage(context.Background(), req)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	require.Equal(t, apierror.CodePermissionDenied, publicErrorInfo(t, err).GetCode())
}

func TestListMessagesMapsCursorAndResponse(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	secondMessage := internalMessage()
	secondMessage.SetId(4002)
	secondMessage.SetAuthorId(1002)
	thirdMessage := internalMessage()
	thirdMessage.SetId(4003)
	svcResp := new(messagev1.ListMessagesResponse)
	svcResp.SetMessages([]*messagev1.Message{internalMessage(), secondMessage, thirdMessage})
	svcResp.SetBeforeCursor(4000)
	svcResp.SetAfterCursor(4002)
	messageClient := &fakeMessageClient{listResponse: svcResp}
	secondProfile := internalUserProfile()
	secondProfile.SetUserId(1002)
	profilesResp := new(userv1.BatchGetUserProfilesResponse)
	profilesResp.SetProfiles([]*userv1.UserProfile{secondProfile, internalUserProfile()})
	userClient := &fakeUserClient{batchGetUserProfilesResponse: profilesResp}
	client, closeServer := newMessageHTTPClientWithUser(t, authenticatorClient, userClient, messageClient, "access-token")
	defer closeServer()

	req := new(apiv1.ListMessagesRequest)
	req.SetChannelId(2001)
	req.SetAround(4001)
	req.SetLimit(25)
	resp, err := client.ListMessages(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(2001), messageClient.listRequest.GetChannelId())
	require.Equal(t, int64(1001), messageClient.listRequest.GetUserId())
	require.True(t, messageClient.listRequest.HasAround())
	require.Equal(t, int64(4001), messageClient.listRequest.GetAround())
	require.Equal(t, int32(25), messageClient.listRequest.GetLimit())
	require.Len(t, resp.GetMessages(), 3)
	require.Equal(t, []int64{1001, 1002}, userClient.batchGetUserProfilesRequest.GetUserIds())
	require.Equal(t, int64(1001), resp.GetMessages()[0].GetAuthor().GetUserId())
	require.Equal(t, int64(1002), resp.GetMessages()[1].GetAuthor().GetUserId())
	require.Equal(t, int64(1001), resp.GetMessages()[2].GetAuthor().GetUserId())
	require.Equal(t, int64(4000), resp.GetBeforeCursor())
	require.Equal(t, int64(4002), resp.GetAfterCursor())
}

func TestListMessagesRequiresEveryAuthorProfile(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	svcResp := new(messagev1.ListMessagesResponse)
	svcResp.SetMessages([]*messagev1.Message{internalMessage()})
	messageClient := &fakeMessageClient{listResponse: svcResp}
	profilesResp := new(userv1.BatchGetUserProfilesResponse)
	userClient := &fakeUserClient{batchGetUserProfilesResponse: profilesResp}
	client, closeServer := newMessageHTTPClientWithUser(t, authenticatorClient, userClient, messageClient, "access-token")
	defer closeServer()

	req := new(apiv1.ListMessagesRequest)
	req.SetChannelId(2001)
	_, err := client.ListMessages(t.Context(), req)
	require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
}

func TestListMessagesBeforeCursor(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	svcResp := new(messagev1.ListMessagesResponse)
	svcResp.SetMessages([]*messagev1.Message{internalMessage()})
	svcResp.SetBeforeCursor(3999)
	messageClient := &fakeMessageClient{listResponse: svcResp}
	client, closeServer := newMessageHTTPClient(t, authenticatorClient, messageClient, "access-token")
	defer closeServer()

	req := new(apiv1.ListMessagesRequest)
	req.SetChannelId(2001)
	req.SetBefore(4001)
	req.SetLimit(10)
	resp, err := client.ListMessages(context.Background(), req)
	require.NoError(t, err)
	require.True(t, messageClient.listRequest.HasBefore())
	require.Equal(t, int64(4001), messageClient.listRequest.GetBefore())
	require.Len(t, resp.GetMessages(), 1)
}

func TestListMessagesAfterCursor(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	svcResp := new(messagev1.ListMessagesResponse)
	svcResp.SetMessages([]*messagev1.Message{internalMessage()})
	svcResp.SetAfterCursor(4002)
	messageClient := &fakeMessageClient{listResponse: svcResp}
	client, closeServer := newMessageHTTPClient(t, authenticatorClient, messageClient, "access-token")
	defer closeServer()

	req := new(apiv1.ListMessagesRequest)
	req.SetChannelId(2001)
	req.SetAfter(4001)
	resp, err := client.ListMessages(context.Background(), req)
	require.NoError(t, err)
	require.True(t, messageClient.listRequest.HasAfter())
	require.Equal(t, int64(4001), messageClient.listRequest.GetAfter())
	require.Equal(t, int64(4002), resp.GetAfterCursor())
}

func TestMessageErrorMappings(t *testing.T) {
	tests := map[string]struct {
		err         error
		connectCode connect.Code
		publicCode  string
	}{
		"not found": {
			err:         rpcerror.New(codes.NotFound, rpcerror.MessageDomain, rpcerror.MessageNotFound, "message not found"),
			connectCode: connect.CodeNotFound,
			publicCode:  apierror.CodeNotFound,
		},
		"invalid request": {
			err:         rpcerror.New(codes.InvalidArgument, rpcerror.MessageDomain, rpcerror.MessageInvalidRequest, "invalid"),
			connectCode: connect.CodeInvalidArgument,
			publicCode:  apierror.CodeInvalidArgument,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			authenticatorClient := &fakeAuthenticatorClient{
				verifyResponse: verifyAccessTokenResponse(1001),
			}
			messageClient := &fakeMessageClient{getError: tt.err}
			client, closeServer := newMessageHTTPClient(t, authenticatorClient, messageClient, "access-token")
			defer closeServer()
			req := new(apiv1.GetMessageRequest)
			req.SetMessageId(4001)
			_, err := client.GetMessage(context.Background(), req)
			require.Equal(t, tt.connectCode, connect.CodeOf(err))
			require.Equal(t, tt.publicCode, publicErrorInfo(t, err).GetCode())
		})
	}
}
