package server

import (
	"context"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	apiratelimit "github.com/soasurs/cordis/services/api/v1/ratelimit"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

type messageServer struct {
	svcCtx *svc.ServiceContext
}

func NewMessage(svcCtx *svc.ServiceContext) apiv1connect.MessageServiceHandler {
	return &messageServer{svcCtx: svcCtx}
}

func (s *messageServer) CreateMessage(ctx context.Context, req *apiv1.CreateMessageRequest) (*apiv1.CreateMessageResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	if err := checkUserPolicy(ctx, apiratelimit.PolicyCreateMessageUser, auth.GetUserId()); err != nil {
		return nil, err
	}
	if err := checkResourcePolicy(ctx, apiratelimit.PolicyCreateMessageChannel, req.GetChannelId()); err != nil {
		return nil, err
	}

	svcReq := new(messagev1.CreateMessageRequest)
	svcReq.SetChannelId(req.GetChannelId())
	svcReq.SetAuthorId(auth.GetUserId())
	svcReq.SetContent(req.GetContent())
	svcReq.SetType(messagev1.MessageType(req.GetType()))
	svcReq.SetFlags(req.GetFlags())
	svcReq.SetReferencedMessageId(req.GetReferencedMessageId())
	svcReq.SetReferencedChannelId(req.GetReferencedChannelId())
	svcReq.SetAttachments(attachmentsToMessageService(req.GetAttachments()))
	svcReq.SetMentionUserIds(req.GetMentionUserIds())

	svcResp, err := s.svcCtx.MessageClient.CreateMessage(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.CreateMessageResponse)
	resp.SetMessage(messageToAPI(svcResp.GetMessage()))
	return resp, nil
}

func (s *messageServer) UpdateMessage(ctx context.Context, req *apiv1.UpdateMessageRequest) (*apiv1.UpdateMessageResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}

	svcReq := new(messagev1.UpdateMessageRequest)
	svcReq.SetMessageId(req.GetMessageId())
	svcReq.SetActorUserId(auth.GetUserId())
	if req.HasContent() {
		svcReq.SetContent(req.GetContent())
	}
	if req.HasFlags() {
		svcReq.SetFlags(req.GetFlags())
	}
	if req.HasAttachments() {
		attachments := new(messagev1.AttachmentList)
		attachments.SetAttachments(attachmentsToMessageService(req.GetAttachments().GetAttachments()))
		svcReq.SetAttachments(attachments)
	}
	if req.HasMentions() {
		mentions := new(messagev1.MentionList)
		mentions.SetUserIds(req.GetMentions().GetUserIds())
		svcReq.SetMentions(mentions)
	}

	svcResp, err := s.svcCtx.MessageClient.UpdateMessage(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.UpdateMessageResponse)
	resp.SetMessage(messageToAPI(svcResp.GetMessage()))
	return resp, nil
}

func (s *messageServer) DeleteMessage(ctx context.Context, req *apiv1.DeleteMessageRequest) (*apiv1.DeleteMessageResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}

	svcReq := new(messagev1.DeleteMessageRequest)
	svcReq.SetMessageId(req.GetMessageId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcResp, err := s.svcCtx.MessageClient.DeleteMessage(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.DeleteMessageResponse)
	resp.SetOk(svcResp.GetOk())
	return resp, nil
}

func (s *messageServer) GetMessage(ctx context.Context, req *apiv1.GetMessageRequest) (*apiv1.GetMessageResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}

	svcReq := new(messagev1.GetMessageRequest)
	svcReq.SetMessageId(req.GetMessageId())
	svcReq.SetUserId(auth.GetUserId())
	svcResp, err := s.svcCtx.MessageClient.GetMessage(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.GetMessageResponse)
	resp.SetMessage(messageToAPI(svcResp.GetMessage()))
	return resp, nil
}

func (s *messageServer) ListMessages(ctx context.Context, req *apiv1.ListMessagesRequest) (*apiv1.ListMessagesResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}

	svcReq := new(messagev1.ListMessagesRequest)
	svcReq.SetChannelId(req.GetChannelId())
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetLimit(req.GetLimit())
	if req.HasBefore() {
		svcReq.SetBefore(req.GetBefore())
	}
	if req.HasAfter() {
		svcReq.SetAfter(req.GetAfter())
	}
	if req.HasAround() {
		svcReq.SetAround(req.GetAround())
	}

	svcResp, err := s.svcCtx.MessageClient.ListMessages(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	messages := make([]*apiv1.Message, 0, len(svcResp.GetMessages()))
	for _, message := range svcResp.GetMessages() {
		messages = append(messages, messageToAPI(message))
	}
	resp := new(apiv1.ListMessagesResponse)
	resp.SetMessages(messages)
	resp.SetBeforeCursor(svcResp.GetBeforeCursor())
	resp.SetAfterCursor(svcResp.GetAfterCursor())
	return resp, nil
}

func messageToAPI(message *messagev1.Message) *apiv1.Message {
	if message == nil {
		return nil
	}
	resp := new(apiv1.Message)
	resp.SetId(message.GetId())
	resp.SetChannelId(message.GetChannelId())
	resp.SetAuthorId(message.GetAuthorId())
	resp.SetContent(message.GetContent())
	resp.SetType(apiv1.MessageType(message.GetType()))
	resp.SetFlags(message.GetFlags())
	resp.SetReferencedMessageId(message.GetReferencedMessageId())
	resp.SetReferencedChannelId(message.GetReferencedChannelId())
	resp.SetAttachments(attachmentsToAPI(message.GetAttachments()))
	resp.SetEditedAt(message.GetEditedAt())
	resp.SetCreatedAt(message.GetCreatedAt())
	resp.SetUpdatedAt(message.GetUpdatedAt())
	resp.SetRevision(message.GetRevision())
	return resp
}

func attachmentsToMessageService(attachments []*apiv1.Attachment) []*messagev1.Attachment {
	values := make([]*messagev1.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		if attachment == nil {
			continue
		}
		value := new(messagev1.Attachment)
		value.SetKey(attachment.GetKey())
		value.SetFilename(attachment.GetFilename())
		value.SetSize(attachment.GetSize())
		value.SetContentType(attachment.GetContentType())
		value.SetWidth(attachment.GetWidth())
		value.SetHeight(attachment.GetHeight())
		values = append(values, value)
	}
	return values
}

func attachmentsToAPI(attachments []*messagev1.Attachment) []*apiv1.Attachment {
	values := make([]*apiv1.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		if attachment == nil {
			continue
		}
		value := new(apiv1.Attachment)
		value.SetKey(attachment.GetKey())
		value.SetFilename(attachment.GetFilename())
		value.SetSize(attachment.GetSize())
		value.SetContentType(attachment.GetContentType())
		value.SetWidth(attachment.GetWidth())
		value.SetHeight(attachment.GetHeight())
		values = append(values, value)
	}
	return values
}

func (s *messageServer) CreateDmChannel(ctx context.Context, req *apiv1.CreateDmChannelRequest) (*apiv1.CreateDmChannelResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}

	svcReq := new(messagev1.CreateDmChannelRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetTargetId(req.GetTargetId())
	svcResp, err := s.svcCtx.MessageClient.CreateDmChannel(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.CreateDmChannelResponse)
	resp.SetChannel(dmChannelToAPI(svcResp.GetChannel(), auth.GetUserId()))
	return resp, nil
}

func (s *messageServer) ListDmChannels(ctx context.Context, req *apiv1.ListDmChannelsRequest) (*apiv1.ListDmChannelsResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}

	svcReq := new(messagev1.ListDmChannelsRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetBeforeId(req.GetBeforeId())
	svcReq.SetLimit(req.GetLimit())
	svcResp, err := s.svcCtx.MessageClient.ListDmChannels(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	channels := make([]*apiv1.DmChannel, 0, len(svcResp.GetChannels()))
	for _, channel := range svcResp.GetChannels() {
		channels = append(channels, dmChannelToAPI(channel, auth.GetUserId()))
	}
	resp := new(apiv1.ListDmChannelsResponse)
	resp.SetChannels(channels)
	resp.SetBeforeId(svcResp.GetBeforeId())
	return resp, nil
}

// dmChannelToAPI converts the stored pair into the caller's perspective.
func dmChannelToAPI(channel *messagev1.DmChannel, viewerID int64) *apiv1.DmChannel {
	if channel == nil {
		return nil
	}
	recipientID := channel.GetUserLo()
	if viewerID == channel.GetUserLo() {
		recipientID = channel.GetUserHi()
	}
	resp := new(apiv1.DmChannel)
	resp.SetId(channel.GetId())
	resp.SetRecipientId(recipientID)
	resp.SetCreatedAt(channel.GetCreatedAt())
	return resp
}

func (s *messageServer) AckMessage(ctx context.Context, req *apiv1.AckMessageRequest) (*apiv1.AckMessageResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}

	svcReq := new(messagev1.AckMessageRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetChannelId(req.GetChannelId())
	svcReq.SetMessageId(req.GetMessageId())
	svcResp, err := s.svcCtx.MessageClient.AckMessage(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.AckMessageResponse)
	resp.SetReadState(apiChannelReadState(svcResp.GetReadState()))
	return resp, nil
}

func (s *messageServer) GetReadStates(ctx context.Context, req *apiv1.GetReadStatesRequest) (*apiv1.GetReadStatesResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	release, err := acquireUserConcurrency(ctx, s.svcCtx.ReadStatesLimiter, auth.GetUserId())
	if err != nil {
		return nil, err
	}
	defer release()

	svcReq := new(messagev1.GetReadStatesRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetScope(messagev1.ReadStateScopeType(req.GetScope()))
	svcReq.SetGuildId(req.GetGuildId())
	svcResp, err := s.svcCtx.MessageClient.GetReadStates(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	dmChannels := make([]*apiv1.DmChannel, 0, len(svcResp.GetDmChannels()))
	for _, channel := range svcResp.GetDmChannels() {
		dmChannels = append(dmChannels, dmChannelToAPI(channel, auth.GetUserId()))
	}
	readStates := make([]*apiv1.ChannelReadState, 0, len(svcResp.GetReadStates()))
	for _, state := range svcResp.GetReadStates() {
		readStates = append(readStates, apiChannelReadState(state))
	}
	resp := new(apiv1.GetReadStatesResponse)
	resp.SetDmChannels(dmChannels)
	resp.SetReadStates(readStates)
	return resp, nil
}

func apiChannelReadState(state *messagev1.ChannelReadState) *apiv1.ChannelReadState {
	if state == nil {
		return nil
	}
	resp := new(apiv1.ChannelReadState)
	resp.SetChannelId(state.GetChannelId())
	resp.SetLastMessageId(state.GetLastMessageId())
	resp.SetLastReadMessageId(state.GetLastReadMessageId())
	resp.SetMentionCount(state.GetMentionCount())
	return resp
}
