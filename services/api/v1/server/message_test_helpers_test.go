package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

func newMessageHTTPClient(
	t *testing.T,
	authenticatorClient *fakeAuthenticatorClient,
	messageClient *fakeMessageClient,
	accessToken string,
) (apiv1connect.MessageServiceClient, func()) {
	t.Helper()
	return newMessageHTTPClientWithUser(
		t,
		authenticatorClient,
		new(fakeUserClient),
		messageClient,
		accessToken,
	)
}

func newMessageHTTPClientWithUser(
	t *testing.T,
	authenticatorClient *fakeAuthenticatorClient,
	userClient *fakeUserClient,
	messageClient *fakeMessageClient,
	accessToken string,
) (apiv1connect.MessageServiceClient, func()) {
	t.Helper()

	svcCtx := &svc.ServiceContext{
		AuthenticatorClient: authenticatorClient,
		UserClient:          userClient,
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
	attachment.SetAssetId(101)
	attachment.SetFilename("a.png")
	attachment.SetSize(10)
	attachment.SetContentType("image/png")
	attachment.SetWidth(100)
	attachment.SetHeight(200)
	attachment.SetUrl("https://download.example/101")
	attachment.SetUrlExpiresAt(9001)

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
	resp.SetAuthor(internalUserProfile())
	return resp
}

func createMessageResponse(message *messagev1.Message) *messagev1.CreateMessageResponse {
	resp := new(messagev1.CreateMessageResponse)
	resp.SetMessage(message)
	resp.SetAuthor(internalUserProfile())
	return resp
}

func updateMessageResponse(message *messagev1.Message) *messagev1.UpdateMessageResponse {
	resp := new(messagev1.UpdateMessageResponse)
	resp.SetMessage(message)
	resp.SetAuthor(internalUserProfile())
	return resp
}
