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
	if err := validateAttachments(attachments, s.svcCtx.Cfg.Limits.Attachments()); err != nil {
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
	if err := validateMentionUserIDs(req.GetMentionUserIds(), s.svcCtx.Cfg.Limits.Mentions()); err != nil {
		return nil, err
	}
	audience, err := s.requireChannelPermission(ctx, req.GetChannelId(), req.GetAuthorId(), permissionSendMessages)
	if err != nil {
		return nil, err
	}

	if req.GetReferencedMessageId() != 0 {
		if _, err := s.requireChannelPermission(ctx, req.GetReferencedChannelId(), req.GetAuthorId(), permissionViewChannel); err != nil {
			return nil, err
		}
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
	var authorReadState *model.ChannelReadState
	var authorReadAdvanced bool

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
		authorReadAdvanced, err = txStore.AckMessage(ctx, req.GetAuthorId(), req.GetChannelId(), messageID)
		if err != nil {
			return err
		}
		if !authorReadAdvanced {
			return nil
		}
		states, err := txStore.ListReadyChannelReadStates(ctx, req.GetAuthorId(), []int64{req.GetChannelId()})
		if err != nil {
			return err
		}
		if len(states) != 1 {
			return notFound()
		}
		authorReadState = states[0]
		return nil
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	events, eventErr := newMessageCreatedEvents(created, req.GetMentionUserIds(), audience)
	s.publishEvents(ctx, events, eventErr)
	if authorReadAdvanced {
		s.publishReadStateUpdated(ctx, authorReadState)
	}

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
	current, err := s.svcCtx.Store.GetMessage(ctx, req.GetMessageId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	requiredPermission := permissionViewChannel | permissionSendMessages
	hasModPermission := current.AuthorID != req.GetActorUserId()
	if hasModPermission {
		requiredPermission |= permissionManageMessages
	}
	audience, err := s.requireChannelPermission(ctx, current.ChannelID, req.GetActorUserId(), requiredPermission)
	if err != nil {
		return nil, err
	}

	params := store.UpdateMessageParams{
		MessageID:        req.GetMessageId(),
		ActorUserID:      req.GetActorUserId(),
		HasModPermission: hasModPermission,
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
		if err := validateAttachments(attachments, s.svcCtx.Cfg.Limits.Attachments()); err != nil {
			return nil, err
		}
		params.Attachments = &attachments
	}
	if req.HasMentions() {
		if err := validateMentionUserIDs(req.GetMentions().GetUserIds(), s.svcCtx.Cfg.Limits.Mentions()); err != nil {
			return nil, err
		}
	}

	var updated *model.Message
	var mentionUserIDs []int64
	var previousMentionUserIDs []int64

	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		message, err := txStore.UpdateMessage(ctx, params)
		if err != nil {
			return err
		}
		updated = message

		if req.HasMentions() {
			previousMentionUserIDs, err = txStore.ListMentionUserIDs(ctx, req.GetMessageId())
			if err != nil {
				return err
			}
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

	events, eventErr := newMessageUpdatedEvents(updated, mentionUserIDs, previousMentionUserIDs, audience)
	s.publishEvents(ctx, events, eventErr)

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
	current, err := s.svcCtx.Store.GetMessage(ctx, req.GetMessageId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	requiredPermission := permissionViewChannel | permissionSendMessages
	hasModPermission := current.AuthorID != req.GetActorUserId()
	if hasModPermission {
		requiredPermission |= permissionManageMessages
	}
	audience, err := s.requireChannelPermission(ctx, current.ChannelID, req.GetActorUserId(), requiredPermission)
	if err != nil {
		return nil, err
	}

	var deleted *model.Message
	var mentionUserIDs []int64
	var lastMessageID int64
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		var err error
		mentionUserIDs, err = txStore.ListMentionUserIDs(ctx, req.GetMessageId())
		if err != nil {
			return err
		}
		message, err := txStore.DeleteMessage(ctx, req.GetMessageId(), req.GetActorUserId(), hasModPermission)
		if err != nil {
			return err
		}
		deleted = message
		lastMessageID, err = txStore.GetLastMessageID(ctx, message.ChannelID)
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	events, eventErr := newMessageDeletedEvents(deleted, lastMessageID, mentionUserIDs, audience)
	s.publishEvents(ctx, events, eventErr)

	resp := new(messagev1.DeleteMessageResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *messageServer) GetMessage(ctx context.Context, req *messagev1.GetMessageRequest) (*messagev1.GetMessageResponse, error) {
	if req.GetMessageId() <= 0 {
		return nil, invalidRequest("message id is required")
	}
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	message, err := s.svcCtx.Store.GetMessage(ctx, req.GetMessageId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	if _, err := s.requireChannelPermission(ctx, message.ChannelID, req.GetUserId(), permissionViewChannel); err != nil {
		return nil, err
	}

	resp := new(messagev1.GetMessageResponse)
	resp.SetMessage(messageToProto(message))
	return resp, nil
}

func (s *messageServer) ListMessages(ctx context.Context, req *messagev1.ListMessagesRequest) (*messagev1.ListMessagesResponse, error) {
	if req.GetChannelId() <= 0 {
		return nil, invalidRequest("channel id is required")
	}
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	if _, err := s.requireChannelPermission(ctx, req.GetChannelId(), req.GetUserId(), permissionViewChannel); err != nil {
		return nil, err
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

func (s *messageServer) publishEvents(ctx context.Context, events []messageEvent, buildErr error) {
	if buildErr != nil {
		s.publishEvent(ctx, messageEvent{}, buildErr)
		return
	}
	for _, event := range events {
		s.publishEvent(ctx, event, nil)
	}
}
