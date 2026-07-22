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
	userv1 "github.com/soasurs/cordis/gen/user/v1"
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

	createDmChannelRequest  *messagev1.CreateDmChannelRequest
	createDmChannelResponse *messagev1.CreateDmChannelResponse
	createDmChannelError    error
	listDmChannelsRequest   *messagev1.ListDmChannelsRequest
	listDmChannelsResponse  *messagev1.ListDmChannelsResponse
	listDmChannelsError     error

	ackMessageRequest     *messagev1.AckMessageRequest
	ackMessageResponse    *messagev1.AckMessageResponse
	ackMessageError       error
	getReadStatesRequest  *messagev1.GetReadStatesRequest
	getReadStatesResponse *messagev1.GetReadStatesResponse
	getReadStatesError    error
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

	attachment := new(apiv1.Attachment)
	attachment.SetKey("attachments/a.png")
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
	req.SetMentionUserIds([]int64{1002})

	resp, err := client.CreateMessage(context.Background(), req)
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
	require.Equal(t, int64(1001), resp.GetMessage().GetAuthor().GetUserId())
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
	req.SetMentions(new(apiv1.MentionList))
	resp, err := client.UpdateMessage(context.Background(), req)
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
	svcResp := new(messagev1.ListMessagesResponse)
	svcResp.SetMessages([]*messagev1.Message{internalMessage()})
	svcResp.SetBeforeCursor(4000)
	svcResp.SetAfterCursor(4002)
	messageClient := &fakeMessageClient{listResponse: svcResp}
	client, closeServer := newMessageHTTPClient(t, authenticatorClient, messageClient, "access-token")
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
	require.Len(t, resp.GetMessages(), 1)
	require.Equal(t, int64(4000), resp.GetBeforeCursor())
	require.Equal(t, int64(4002), resp.GetAfterCursor())
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
	message.SetContent("hello")
	message.SetType(messagev1.MessageType_MESSAGE_TYPE_DEFAULT)
	message.SetFlags(int32(messagev1.MessageFlag_MESSAGE_FLAG_SUPPRESS_NOTIFICATIONS))
	message.SetAttachments([]*messagev1.Attachment{attachment})
	message.SetEditedAt(5001)
	message.SetCreatedAt(5000)
	message.SetUpdatedAt(5001)
	message.SetRevision(2)
	author := new(userv1.UserProfile)
	author.SetUserId(1001)
	author.SetName("Alice")
	author.SetUsername("alice")
	author.SetAvatarUri("avatar://alice")
	author.SetCreatedAt(100)
	author.SetUpdatedAt(200)
	message.SetAuthor(author)
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

func (f *fakeMessageClient) CreateDmChannel(_ context.Context, req *messagev1.CreateDmChannelRequest, _ ...grpc.CallOption) (*messagev1.CreateDmChannelResponse, error) {
	f.createDmChannelRequest = req
	if f.createDmChannelError != nil {
		return nil, f.createDmChannelError
	}
	return f.createDmChannelResponse, nil
}

func (f *fakeMessageClient) ListDmChannels(_ context.Context, req *messagev1.ListDmChannelsRequest, _ ...grpc.CallOption) (*messagev1.ListDmChannelsResponse, error) {
	f.listDmChannelsRequest = req
	if f.listDmChannelsError != nil {
		return nil, f.listDmChannelsError
	}
	return f.listDmChannelsResponse, nil
}

func (f *fakeMessageClient) AckMessage(_ context.Context, req *messagev1.AckMessageRequest, _ ...grpc.CallOption) (*messagev1.AckMessageResponse, error) {
	f.ackMessageRequest = req
	if f.ackMessageError != nil {
		return nil, f.ackMessageError
	}
	return f.ackMessageResponse, nil
}

func (f *fakeMessageClient) GetReadStates(_ context.Context, req *messagev1.GetReadStatesRequest, _ ...grpc.CallOption) (*messagev1.GetReadStatesResponse, error) {
	f.getReadStatesRequest = req
	if f.getReadStatesError != nil {
		return nil, f.getReadStatesError
	}
	return f.getReadStatesResponse, nil
}

func TestCreateDmChannelUsesAuthenticatedUser(t *testing.T) {
	channel := new(messagev1.DmChannel)
	channel.SetId(500)
	channel.SetUserLo(1001)
	channel.SetUserHi(2002)
	channel.SetCreatedAt(4001)
	svcResp := new(messagev1.CreateDmChannelResponse)
	svcResp.SetChannel(channel)

	messageClient := &fakeMessageClient{createDmChannelResponse: svcResp}
	client, closeServer := newMessageHTTPClient(t, &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}, messageClient, "access-token")
	defer closeServer()

	req := new(apiv1.CreateDmChannelRequest)
	req.SetTargetId(2002)
	resp, err := client.CreateDmChannel(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), messageClient.createDmChannelRequest.GetUserId())
	require.Equal(t, int64(2002), messageClient.createDmChannelRequest.GetTargetId())
	// The stored pair is translated into the caller's perspective.
	require.Equal(t, int64(2002), resp.GetChannel().GetRecipientId())
	require.Equal(t, int64(500), resp.GetChannel().GetId())
}

func TestListDmChannelsMapsPerspective(t *testing.T) {
	channel := new(messagev1.DmChannel)
	channel.SetId(500)
	channel.SetUserLo(42)
	channel.SetUserHi(1001)
	svcResp := new(messagev1.ListDmChannelsResponse)
	svcResp.SetChannels([]*messagev1.DmChannel{channel})
	svcResp.SetBeforeId(500)

	messageClient := &fakeMessageClient{listDmChannelsResponse: svcResp}
	client, closeServer := newMessageHTTPClient(t, &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}, messageClient, "access-token")
	defer closeServer()

	req := new(apiv1.ListDmChannelsRequest)
	req.SetBeforeId(600)
	req.SetLimit(10)
	resp, err := client.ListDmChannels(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), messageClient.listDmChannelsRequest.GetUserId())
	require.Equal(t, int64(600), messageClient.listDmChannelsRequest.GetBeforeId())
	require.Len(t, resp.GetChannels(), 1)
	// The caller is user_hi here, so the recipient is user_lo.
	require.Equal(t, int64(42), resp.GetChannels()[0].GetRecipientId())
	require.Equal(t, int64(500), resp.GetBeforeId())
}

func TestAckMessageUsesAuthenticatedUser(t *testing.T) {
	ackRes := new(messagev1.AckMessageResponse)
	readState := new(messagev1.ChannelReadState)
	readState.SetChannelId(2001)
	readState.SetLastMessageId(3002)
	readState.SetLastReadMessageId(3001)
	readState.SetMentionCount(2)
	ackRes.SetReadState(readState)
	messageClient := &fakeMessageClient{ackMessageResponse: ackRes}
	client, closeServer := newMessageHTTPClient(t, &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}, messageClient, "access-token")
	defer closeServer()

	req := new(apiv1.AckMessageRequest)
	req.SetChannelId(2001)
	req.SetMessageId(3001)
	resp, err := client.AckMessage(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(2001), resp.GetReadState().GetChannelId())
	require.Equal(t, int64(3002), resp.GetReadState().GetLastMessageId())
	require.Equal(t, int64(3001), resp.GetReadState().GetLastReadMessageId())
	require.Equal(t, int32(2), resp.GetReadState().GetMentionCount())
	require.Equal(t, int64(1001), messageClient.ackMessageRequest.GetUserId())
	require.Equal(t, int64(2001), messageClient.ackMessageRequest.GetChannelId())
	require.Equal(t, int64(3001), messageClient.ackMessageRequest.GetMessageId())
}

func TestGetReadStatesUsesAuthenticatedScopedRequest(t *testing.T) {
	dm := new(messagev1.DmChannel)
	dm.SetId(500)
	dm.SetUserLo(1001)
	dm.SetUserHi(2002)
	state := new(messagev1.ChannelReadState)
	state.SetChannelId(500)
	state.SetLastMessageId(600)
	svcResp := new(messagev1.GetReadStatesResponse)
	svcResp.SetDmChannels([]*messagev1.DmChannel{dm})
	svcResp.SetReadStates([]*messagev1.ChannelReadState{state})
	messageClient := &fakeMessageClient{getReadStatesResponse: svcResp}
	client, closeServer := newMessageHTTPClient(t, &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}, messageClient, "access-token")
	defer closeServer()

	req := new(apiv1.GetReadStatesRequest)
	req.SetScope(apiv1.ReadStateScopeType_READ_STATE_SCOPE_TYPE_ALL_DMS)
	resp, err := client.GetReadStates(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), messageClient.getReadStatesRequest.GetUserId())
	require.Equal(t, messagev1.ReadStateScopeType_READ_STATE_SCOPE_TYPE_ALL_DMS, messageClient.getReadStatesRequest.GetScope())
	require.Equal(t, int64(2002), resp.GetDmChannels()[0].GetRecipientId())
	require.Equal(t, int64(600), resp.GetReadStates()[0].GetLastMessageId())
}
