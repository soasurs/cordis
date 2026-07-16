package server

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
)

func (s *messageServer) CreateMessage(ctx context.Context, req *messagev1.CreateMessageRequest) (*messagev1.CreateMessageResponse, error) {
	if req.GetChannelId() <= 0 {
		return nil, invalidRequest("channel id is required")
	}
	if req.GetAuthorId() <= 0 {
		return nil, invalidRequest("author id is required")
	}
	if err := validateContent(req.GetContent()); err != nil {
		return nil, err
	}
	attachments := attachmentsFromProto(req.GetAttachments())
	if err := validateAttachments(attachments); err != nil {
		return nil, err
	}
	if err := validateFlags(req.GetFlags()); err != nil {
		return nil, err
	}
	if req.GetContent() == "" && len(attachments) == 0 {
		return nil, invalidRequest("content or attachments are required")
	}

	messageType, err := normalizeMessageType(req.GetType())
	if err != nil {
		return nil, err
	}
	if messageType == messagev1.MessageType_MESSAGE_TYPE_REPLY && req.GetReferencedMessageId() <= 0 {
		return nil, invalidRequest("referenced message id is required")
	}
	if messageType != messagev1.MessageType_MESSAGE_TYPE_REPLY && req.GetReferencedMessageId() != 0 {
		return nil, invalidRequest("referenced message is only valid for reply messages")
	}
	if (req.GetReferencedMessageId() == 0) != (req.GetReferencedChannelId() == 0) {
		return nil, invalidRequest("referenced message and channel must be set together")
	}
	if err := validateMentionUserIDs(req.GetMentionUserIds()); err != nil {
		return nil, err
	}

	if req.GetReferencedMessageId() != 0 {
		referencedMessage, err := s.svcCtx.Store.GetMessage(ctx, req.GetReferencedMessageId())
		if err != nil {
			return nil, mapStoreError(err)
		}
		if referencedMessage.ChannelID != req.GetReferencedChannelId() {
			return nil, invalidRequest("referenced channel does not match referenced message")
		}
	}

	messageID := s.svcCtx.Snowflake.Generate().Int64()
	var created *model.Message

	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		message, err := txStore.CreateMessage(ctx, store.CreateMessageParams{
			MessageID:           messageID,
			ChannelID:           req.GetChannelId(),
			AuthorID:            req.GetAuthorId(),
			Content:             req.GetContent(),
			Type:                int32(messageType),
			Flags:               req.GetFlags(),
			ReferencedMessageID: req.GetReferencedMessageId(),
			ReferencedChannelID: req.GetReferencedChannelId(),
			Attachments:         attachments,
		})
		if err != nil {
			return err
		}
		created = message

		if err := txStore.ReplaceMessageMentions(ctx, messageID, req.GetMentionUserIds()); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	event, eventErr := newMessageCreatedEvent(created, req.GetMentionUserIds())
	s.publishEvent(ctx, event, eventErr)

	resp := new(messagev1.CreateMessageResponse)
	resp.SetMessage(messageToProto(created))
	return resp, nil
}

func (s *messageServer) UpdateMessage(ctx context.Context, req *messagev1.UpdateMessageRequest) (*messagev1.UpdateMessageResponse, error) {
	if req.GetMessageId() <= 0 {
		return nil, invalidRequest("message id is required")
	}
	if req.GetActorUserId() <= 0 {
		return nil, invalidRequest("actor user id is required")
	}
	if !req.HasContent() && !req.HasFlags() && !req.HasAttachments() && !req.HasMentions() {
		return nil, invalidRequest("at least one field must be updated")
	}

	params := store.UpdateMessageParams{
		MessageID:        req.GetMessageId(),
		ActorUserID:      req.GetActorUserId(),
		HasModPermission: req.GetHasPermission(),
	}
	if req.HasContent() {
		content := req.GetContent()
		if err := validateContent(content); err != nil {
			return nil, err
		}
		params.Content = &content
	}
	if req.HasFlags() {
		flags := req.GetFlags()
		if err := validateFlags(flags); err != nil {
			return nil, err
		}
		params.Flags = &flags
	}
	if req.HasAttachments() {
		attachments := attachmentsFromProto(req.GetAttachments().GetAttachments())
		if err := validateAttachments(attachments); err != nil {
			return nil, err
		}
		params.Attachments = &attachments
	}
	if req.HasMentions() {
		if err := validateMentionUserIDs(req.GetMentions().GetUserIds()); err != nil {
			return nil, err
		}
	}

	var updated *model.Message
	var mentionUserIDs []int64

	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		message, err := txStore.UpdateMessage(ctx, params)
		if err != nil {
			return err
		}
		updated = message

		if req.HasMentions() {
			if err := txStore.ReplaceMessageMentions(ctx, req.GetMessageId(), req.GetMentions().GetUserIds()); err != nil {
				return err
			}
		}

		if req.HasMentions() {
			mentionUserIDs = req.GetMentions().GetUserIds()
		} else {
			mentionUserIDs, err = txStore.ListMentionUserIDs(ctx, req.GetMessageId())
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	event, eventErr := newMessageUpdatedEvent(updated, mentionUserIDs)
	s.publishEvent(ctx, event, eventErr)

	resp := new(messagev1.UpdateMessageResponse)
	resp.SetMessage(messageToProto(updated))
	return resp, nil
}

func (s *messageServer) DeleteMessage(ctx context.Context, req *messagev1.DeleteMessageRequest) (*messagev1.DeleteMessageResponse, error) {
	if req.GetMessageId() <= 0 {
		return nil, invalidRequest("message id is required")
	}
	if req.GetActorUserId() <= 0 {
		return nil, invalidRequest("actor user id is required")
	}

	var deleted *model.Message
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		message, err := txStore.DeleteMessage(ctx, req.GetMessageId(), req.GetActorUserId(), req.GetHasPermission())
		if err != nil {
			return err
		}
		deleted = message
		return nil
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	event, eventErr := newMessageDeletedEvent(deleted)
	s.publishEvent(ctx, event, eventErr)

	resp := new(messagev1.DeleteMessageResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *messageServer) GetMessage(ctx context.Context, req *messagev1.GetMessageRequest) (*messagev1.GetMessageResponse, error) {
	if req.GetMessageId() <= 0 {
		return nil, invalidRequest("message id is required")
	}
	message, err := s.svcCtx.Store.GetMessage(ctx, req.GetMessageId())
	if err != nil {
		return nil, mapStoreError(err)
	}

	resp := new(messagev1.GetMessageResponse)
	resp.SetMessage(messageToProto(message))
	return resp, nil
}

func (s *messageServer) ListMessages(ctx context.Context, req *messagev1.ListMessagesRequest) (*messagev1.ListMessagesResponse, error) {
	if req.GetChannelId() <= 0 {
		return nil, invalidRequest("channel id is required")
	}
	limit, err := normalizeLimit(req.GetLimit(), defaultMessageLimit, maxMessageLimit)
	if err != nil {
		return nil, err
	}

	params := store.ListMessagesParams{
		ChannelID: req.GetChannelId(),
		Limit:     limit,
	}
	switch {
	case req.HasBefore():
		if req.GetBefore() <= 0 {
			return nil, invalidRequest("before cursor must be positive")
		}
		params.Before = req.GetBefore()
	case req.HasAfter():
		if req.GetAfter() <= 0 {
			return nil, invalidRequest("after cursor must be positive")
		}
		params.After = req.GetAfter()
	case req.HasAround():
		if req.GetAround() <= 0 {
			return nil, invalidRequest("around cursor must be positive")
		}
		params.Around = req.GetAround()
	}

	messages, err := s.svcCtx.Store.ListMessages(ctx, params)
	if err != nil {
		return nil, err
	}
	resp := new(messagev1.ListMessagesResponse)
	resp.SetMessages(messagesToProto(messages))
	setListCursors(resp, messages)
	return resp, nil
}

func messagesToProto(messages []*model.Message) []*messagev1.Message {
	values := make([]*messagev1.Message, 0, len(messages))
	for _, message := range messages {
		values = append(values, messageToProto(message))
	}
	return values
}

func setListCursors(resp *messagev1.ListMessagesResponse, messages []*model.Message) {
	if len(messages) == 0 {
		return
	}
	minID := messages[0].ID
	maxID := messages[0].ID
	for _, message := range messages[1:] {
		if message.ID < minID {
			minID = message.ID
		}
		if message.ID > maxID {
			maxID = message.ID
		}
	}
	resp.SetBeforeCursor(minID)
	resp.SetAfterCursor(maxID)
}

func (s *messageServer) publishEvent(ctx context.Context, event messageEvent, buildErr error) {
	if buildErr != nil {
		logx.WithContext(ctx).Errorw("build message event",
			logx.Field("error", buildErr),
		)
		return
	}
	if s.svcCtx.Publisher == nil {
		return
	}

	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.svcCtx.Cfg.Kafka.PublishTimeout())
	defer cancel()
	if err := s.svcCtx.Publisher.Publish(publishCtx, event.Key, event.Payload); err != nil {
		logx.WithContext(ctx).Errorw("publish message event",
			logx.Field("key", string(event.Key)),
			logx.Field("error", err),
		)
	}
}
