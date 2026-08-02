package server

import (
	"context"

	"google.golang.org/grpc"

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
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
	uploadRequest  *messagev1.CreateAttachmentUploadRequest
	uploadResponse *messagev1.CreateAttachmentUploadResponse
	uploadError    error

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

func (f *fakeMessageClient) CreateAttachmentUpload(
	_ context.Context,
	req *messagev1.CreateAttachmentUploadRequest,
	_ ...grpc.CallOption,
) (*messagev1.CreateAttachmentUploadResponse, error) {
	f.uploadRequest = req
	return f.uploadResponse, f.uploadError
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
