package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

type fakeMessageClient struct {
	messagev1.MessageServiceClient
	createRequest  *messagev1.CreateMessageRequest
	createResponse *messagev1.CreateMessageResponse
	createError    error
	updateRequest  *messagev1.UpdateMessageRequest
	updateResponse *messagev1.UpdateMessageResponse
	updateError    error
	deleteRequest  *messagev1.DeleteMessageRequest
	deleteResponse *messagev1.DeleteMessageResponse
	deleteError    error
	getRequest     *messagev1.GetMessageRequest
	getResponse    *messagev1.GetMessageResponse
	getError       error
	listRequest    *messagev1.ListMessagesRequest
	listResponse   *messagev1.ListMessagesResponse
	listError      error
}

func (f *fakeMessageClient) CreateMessage(_ context.Context, req *messagev1.CreateMessageRequest, _ ...grpc.CallOption) (*messagev1.CreateMessageResponse, error) {
	f.createRequest = req
	return f.createResponse, f.createError
}

func (f *fakeMessageClient) UpdateMessage(_ context.Context, req *messagev1.UpdateMessageRequest, _ ...grpc.CallOption) (*messagev1.UpdateMessageResponse, error) {
	f.updateRequest = req
	return f.updateResponse, f.updateError
}

func (f *fakeMessageClient) DeleteMessage(_ context.Context, req *messagev1.DeleteMessageRequest, _ ...grpc.CallOption) (*messagev1.DeleteMessageResponse, error) {
	f.deleteRequest = req
	return f.deleteResponse, f.deleteError
}

func (f *fakeMessageClient) GetMessage(_ context.Context, req *messagev1.GetMessageRequest, _ ...grpc.CallOption) (*messagev1.GetMessageResponse, error) {
	f.getRequest = req
	return f.getResponse, f.getError
}

func (f *fakeMessageClient) ListMessages(_ context.Context, req *messagev1.ListMessagesRequest, _ ...grpc.CallOption) (*messagev1.ListMessagesResponse, error) {
	f.listRequest = req
	return f.listResponse, f.listError
}

func TestCreateMessageUsesAuthenticatedAuthor(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	messageClient := &fakeMessageClient{
		createResponse: createMessageResponse(internalMessage()),
	}
	client, closeServer := newMessageHTTPClient(t, authenticatorClient, messageClient, "access-token")
	defer closeServer()

	resp, err := client.CreateMessage(context.Background(), &apiv1.CreateMessageRequest{
		ChannelId:           new(int64(2001)),
		Content:             new("hello"),
		Type:                new(apiv1.MessageType_MESSAGE_TYPE_REPLY),
		Flags:               new(int32(apiv1.MessageFlag_MESSAGE_FLAG_SUPPRESS_NOTIFICATIONS)),
		ReferencedMessageId: new(int64(3000)),
		ReferencedChannelId: new(int64(2001)),
		Attachments: []*apiv1.Attachment{
			{
				Key:         new("attachments/a.png"),
				Filename:    new("a.png"),
				Size:        new(int64(10)),
				ContentType: new("image/png"),
				Width:       new(int32(100)),
				Height:      new(int32(200)),
			},
		},
		MentionUserIds: []int64{1002},
	})
	require.NoError(t, err)

	require.Equal(t, int64(1001), messageClient.createRequest.GetAuthorId())
	require.Equal(t, int64(2001), messageClient.createRequest.GetChannelId())
	require.Equal(t, messagev1.MessageType_MESSAGE_TYPE_REPLY, messageClient.createRequest.GetType())
	require.Equal(t, int32(messagev1.MessageFlag_MESSAGE_FLAG_SUPPRESS_NOTIFICATIONS), messageClient.createRequest.GetFlags())
	require.Equal(t, int64(3000), messageClient.createRequest.GetReferencedMessageId())
	require.Equal(t, []int64{1002}, messageClient.createRequest.GetMentionUserIds())
	require.Equal(t, "attachments/a.png", messageClient.createRequest.GetAttachments()[0].GetKey())
	require.Equal(t, int64(4001), resp.GetMessage().GetId())
	require.Equal(t, int64(2), resp.GetMessage().GetRevision())
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

	resp, err := client.UpdateMessage(context.Background(), &apiv1.UpdateMessageRequest{
		MessageId:   new(int64(4001)),
		Content:     new(""),
		Attachments: &apiv1.AttachmentList{},
		Mentions:    &apiv1.MentionList{},
	})
	require.NoError(t, err)

	require.Equal(t, int64(1001), messageClient.updateRequest.GetActorUserId())
	require.True(t, messageClient.updateRequest.HasContent())
	require.Equal(t, "", messageClient.updateRequest.GetContent())
	require.False(t, messageClient.updateRequest.HasFlags())
	require.True(t, messageClient.updateRequest.HasAttachments())
	require.Empty(t, messageClient.updateRequest.GetAttachments().GetAttachments())
	require.True(t, messageClient.updateRequest.HasMentions())
	require.Empty(t, messageClient.updateRequest.GetMentions().GetUserIds())
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

	resp, err := client.DeleteMessage(context.Background(), &apiv1.DeleteMessageRequest{
		MessageId: new(int64(4001)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(4001), messageClient.deleteRequest.GetMessageId())
	require.Equal(t, int64(1001), messageClient.deleteRequest.GetActorUserId())
	require.True(t, resp.GetOk())
}

func TestGetMessageRequiresAccessToken(t *testing.T) {
	client, closeServer := newMessageHTTPClient(t, &fakeAuthenticatorClient{}, &fakeMessageClient{}, "")
	defer closeServer()

	_, err := client.GetMessage(context.Background(), &apiv1.GetMessageRequest{
		MessageId: new(int64(4001)),
	})
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

func TestGetMessageUsesAuthenticatedUser(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{verifyResponse: verifyAccessTokenResponse(1001)}
	messageClient := &fakeMessageClient{getResponse: createGetMessageResponse(internalMessage())}
	client, closeServer := newMessageHTTPClient(t, authenticatorClient, messageClient, "access-token")
	defer closeServer()

	_, err := client.GetMessage(context.Background(), &apiv1.GetMessageRequest{MessageId: new(int64(4001))})
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

	_, err := client.UpdateMessage(context.Background(), &apiv1.UpdateMessageRequest{
		MessageId: new(int64(4001)),
		Content:   new("updated"),
	})
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	require.Equal(t, apierror.CodePermissionDenied, publicErrorInfo(t, err).GetCode())
}

func TestListMessagesMapsCursorAndResponse(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	svcResp := new(messagev1.ListMessagesResponse)
	svcResp.SetMessages([]*messagev1.Message{internalMessage()})
	svcResp.SetBeforeCursor(4000)
	svcResp.SetAfterCursor(4002)
	messageClient := &fakeMessageClient{listResponse: svcResp}
	client, closeServer := newMessageHTTPClient(t, authenticatorClient, messageClient, "access-token")
	defer closeServer()

	resp, err := client.ListMessages(context.Background(), &apiv1.ListMessagesRequest{
		ChannelId: new(int64(2001)),
		Cursor:    &apiv1.ListMessagesRequest_Around{Around: 4001},
		Limit:     new(int32(25)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(2001), messageClient.listRequest.GetChannelId())
	require.Equal(t, int64(1001), messageClient.listRequest.GetUserId())
	require.True(t, messageClient.listRequest.HasAround())
	require.Equal(t, int64(4001), messageClient.listRequest.GetAround())
	require.Equal(t, int32(25), messageClient.listRequest.GetLimit())
	require.Len(t, resp.GetMessages(), 1)
	require.Equal(t, int64(4000), resp.GetBeforeCursor())
	require.Equal(t, int64(4002), resp.GetAfterCursor())
}

func newMessageHTTPClient(
	t *testing.T,
	authenticatorClient *fakeAuthenticatorClient,
	messageClient *fakeMessageClient,
	accessToken string,
) (apiv1connect.MessageServiceClient, func()) {
	t.Helper()

	svcCtx := &svc.ServiceContext{
		AuthenticatorClient: authenticatorClient,
		MessageClient:       messageClient,
	}
	path, handler := apiv1connect.NewMessageServiceHandler(NewMessage(svcCtx))
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	httpServer := httptest.NewServer(mux)

	httpClient := &http.Client{Transport: bearerRoundTripper{
		base:        http.DefaultTransport,
		accessToken: accessToken,
	}}
	return apiv1connect.NewMessageServiceClient(httpClient, httpServer.URL), httpServer.Close
}

func internalMessage() *messagev1.Message {
	attachment := new(messagev1.Attachment)
	attachment.SetKey("attachments/a.png")
	attachment.SetFilename("a.png")
	attachment.SetSize(10)
	attachment.SetContentType("image/png")
	attachment.SetWidth(100)
	attachment.SetHeight(200)

	message := new(messagev1.Message)
	message.SetId(4001)
	message.SetChannelId(2001)
	message.SetAuthorId(1001)
	message.SetContent("hello")
	message.SetType(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)
	message.SetFlags(int32(messagev1.MessageFlag_MESSAGE_FLAG_SUPPRESS_NOTIFICATIONS))
	message.SetAttachments([]*messagev1.Attachment{attachment})
	message.SetEditedAt(5001)
	message.SetCreatedAt(5000)
	message.SetUpdatedAt(5001)
	message.SetRevision(2)
	return message
}

func createGetMessageResponse(message *messagev1.Message) *messagev1.GetMessageResponse {
	resp := new(messagev1.GetMessageResponse)
	resp.SetMessage(message)
	return resp
}

func createMessageResponse(message *messagev1.Message) *messagev1.CreateMessageResponse {
	resp := new(messagev1.CreateMessageResponse)
	resp.SetMessage(message)
	return resp
}

func updateMessageResponse(message *messagev1.Message) *messagev1.UpdateMessageResponse {
	resp := new(messagev1.UpdateMessageResponse)
	resp.SetMessage(message)
	return resp
}
