package server

import (
	"bytes"
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/services/message/v1/internal/eventoutbox"
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
	if err := validateIdempotencyKey(req, s.svcCtx.Cfg.Idempotency.KeyLength()); err != nil {
		return nil, err
	}
	if err := validateContent(req.GetContent()); err != nil {
		return nil, err
	}
	if len(req.GetAttachments()) > s.svcCtx.Cfg.Limits.Attachments() {
		return nil, resourceLimitExceeded("attachment limit exceeded")
	}
	if err := validateFlags(req.GetFlags()); err != nil {
		return nil, err
	}
	if req.GetContent() == "" && len(req.GetAttachments()) == 0 {
		return nil, invalidRequest("content or attachments are required")
	}

	messageType, err := normalizeMessageType(req.GetType())
	if err != nil {
		return nil, err
	}
	attachments, err := s.resolveAttachments(
		ctx,
		req.GetChannelId(),
		req.GetAuthorId(),
		req.GetAttachments(),
	)
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
	audience, err := s.requireChannelPermission(ctx, req.GetChannelId(), req.GetAuthorId(), permissionSendMessages)
	if err != nil {
		return nil, err
	}
	mentions, err := s.resolveMentions(ctx, req.GetContent(), audience, req.GetAuthorId())
	if err != nil {
		return nil, err
	}
	if err := validateMentionsSet(mentions, s.svcCtx.Cfg.Limits.Mentions()); err != nil {
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
	author, err := s.getAuthor(ctx, req.GetAuthorId())
	if err != nil {
		return nil, err
	}

	var requestHash []byte
	if req.HasIdempotencyKey() {
		requestHash, err = createMessageRequestHash(
			req.GetChannelId(),
			req.GetContent(),
			messageType,
			req.GetFlags(),
			req.GetReferencedMessageId(),
			req.GetReferencedChannelId(),
			attachments,
			mentions,
		)
		if err != nil {
			return nil, err
		}
	}

	messageID := s.svcCtx.Snowflake.Generate().Int64()
	var created *model.Message
	createdNewMessage := !req.HasIdempotencyKey()

	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		if req.HasIdempotencyKey() {
			now := time.Now().UnixMilli()
			claim, err := txStore.ClaimMessageIdempotency(ctx, store.ClaimMessageIdempotencyParams{
				ActorUserID:    req.GetAuthorId(),
				Operation:      createMessageOperation,
				IdempotencyKey: req.GetIdempotencyKey(),
				RequestHash:    requestHash,
				MessageID:      messageID,
				CreatedAt:      now,
				ExpiresAt:      now + s.svcCtx.Cfg.Idempotency.CreateMessageTTL().Milliseconds(),
			})
			if err != nil {
				return err
			}
			if !bytes.Equal(claim.RequestHash, requestHash) {
				return idempotencyKeyReused()
			}
			if !claim.Claimed {
				created, err = txStore.GetMessage(ctx, claim.MessageID)
				if err != nil {
					return err
				}
				stored, err := txStore.ListMessageMentions(ctx, claim.MessageID)
				if err != nil {
					return err
				}
				created.Mentions = *stored
				return nil
			}
			createdNewMessage = true
		}

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

		if err := txStore.ReplaceMessageMentions(ctx, messageID, mentions); err != nil {
			return err
		}
		created.Mentions = mentions

		events, err := newMessageCreatedEvents(
			created,
			author,
			mentions,
			audience,
			s.svcCtx.Snowflake.Generate().Int64(),
		)
		if err != nil {
			return err
		}
		return s.enqueueMessageEvents(
			ctx,
			txStore,
			events,
			s.svcCtx.Cfg.Kafka.Topic,
			s.svcCtx.Cfg.Outbox.MessageShards(),
			eventoutbox.MessageNotifyChannel,
		)
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	if createdNewMessage {
		copyAttachmentURLs(created.Attachments, attachments)
	} else if err := s.hydrateAttachmentURLs(ctx, created); err != nil {
		return nil, err
	}

	resp := new(messagev1.CreateMessageResponse)
	resp.SetMessage(messageToProto(created))
	resp.SetAuthor(author)
	return resp, nil
}

func (s *messageServer) UpdateMessage(ctx context.Context, req *messagev1.UpdateMessageRequest) (*messagev1.UpdateMessageResponse, error) {
	if req.GetMessageId() <= 0 {
		return nil, invalidRequest("message id is required")
	}
	if req.GetActorUserId() <= 0 {
		return nil, invalidRequest("actor user id is required")
	}
	if !req.HasContent() && !req.HasFlags() && !req.HasAttachments() {
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
	author, err := s.getAuthor(ctx, current.AuthorID)
	if err != nil {
		return nil, err
	}

	params := store.UpdateMessageParams{
		MessageID:        req.GetMessageId(),
		ActorUserID:      req.GetActorUserId(),
		HasModPermission: hasModPermission,
	}
	attachmentURLSource := current.Attachments
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
		if len(req.GetAttachments().GetAttachments()) > s.svcCtx.Cfg.Limits.Attachments() {
			return nil, resourceLimitExceeded("attachment limit exceeded")
		}
		attachments, err := s.resolveAttachments(
			ctx,
			current.ChannelID,
			req.GetActorUserId(),
			req.GetAttachments().GetAttachments(),
		)
		if err != nil {
			return nil, err
		}
		params.Attachments = &attachments
		attachmentURLSource = attachments
	}
	var newMentions model.MessageMentions
	if req.HasContent() {
		newMentions, err = s.resolveMentions(ctx, req.GetContent(), audience, req.GetActorUserId())
		if err != nil {
			return nil, err
		}
		if err := validateMentionsSet(newMentions, s.svcCtx.Cfg.Limits.Mentions()); err != nil {
			return nil, err
		}
	}
	if !req.HasAttachments() {
		if err := s.hydrateAttachmentURLs(ctx, current); err != nil {
			return nil, err
		}
		attachmentURLSource = current.Attachments
	}

	var updated *model.Message
	var mentions model.MessageMentions
	var previousMentions model.MessageMentions

	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		message, err := txStore.UpdateMessage(ctx, params)
		if err != nil {
			return err
		}
		updated = message

		if req.HasContent() {
			stored, err := txStore.ListMessageMentions(ctx, req.GetMessageId())
			if err != nil {
				return err
			}
			previousMentions = *stored
			if err := txStore.ReplaceMessageMentions(ctx, req.GetMessageId(), newMentions); err != nil {
				return err
			}
			mentions = newMentions
		} else {
			stored, err := txStore.ListMessageMentions(ctx, req.GetMessageId())
			if err != nil {
				return err
			}
			mentions = *stored
		}
		events, err := newMessageUpdatedEvents(
			updated,
			author,
			mentions,
			previousMentions,
			req.HasContent(),
			audience,
			s.svcCtx.Snowflake.Generate().Int64(),
		)
		if err != nil {
			return err
		}
		return s.enqueueMessageEvents(
			ctx,
			txStore,
			events,
			s.svcCtx.Cfg.Kafka.Topic,
			s.svcCtx.Cfg.Outbox.MessageShards(),
			eventoutbox.MessageNotifyChannel,
		)
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	updated.Mentions = mentions
	copyAttachmentURLs(updated.Attachments, attachmentURLSource)

	resp := new(messagev1.UpdateMessageResponse)
	resp.SetMessage(messageToProto(updated))
	resp.SetAuthor(author)
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
	var mentions model.MessageMentions
	var lastMessageID int64
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		var err error
		stored, err := txStore.ListMessageMentions(ctx, req.GetMessageId())
		if err != nil {
			return err
		}
		mentions = *stored
		message, err := txStore.DeleteMessage(ctx, req.GetMessageId(), req.GetActorUserId(), hasModPermission)
		if err != nil {
			return err
		}
		if err := txStore.ReplaceMessageMentions(ctx, req.GetMessageId(), model.MessageMentions{}); err != nil {
			return err
		}
		deleted = message
		lastMessageID, err = txStore.GetLastMessageID(ctx, message.ChannelID)
		if err != nil {
			return err
		}
		events, err := newMessageDeletedEvents(
			deleted,
			lastMessageID,
			mentions,
			audience,
			s.svcCtx.Snowflake.Generate().Int64(),
		)
		if err != nil {
			return err
		}
		return s.enqueueMessageEvents(
			ctx,
			txStore,
			events,
			s.svcCtx.Cfg.Kafka.Topic,
			s.svcCtx.Cfg.Outbox.MessageShards(),
			eventoutbox.MessageNotifyChannel,
		)
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

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
	if err := s.hydrateAttachmentURLs(ctx, message); err != nil {
		return nil, err
	}
	mentions, err := s.svcCtx.Store.ListMessageMentions(ctx, message.ID)
	if err != nil {
		return nil, mapStoreError(err)
	}
	message.Mentions.UserIDs = mentions.UserIDs
	message.Mentions.RoleIDs = mentions.RoleIDs
	author, err := s.getAuthor(ctx, message.AuthorID)
	if err != nil {
		return nil, err
	}

	resp := new(messagev1.GetMessageResponse)
	resp.SetMessage(messageToProto(message))
	resp.SetAuthor(author)
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
	if err := s.hydrateAttachmentURLs(ctx, messages...); err != nil {
		return nil, err
	}
	messageIDs := make([]int64, 0, len(messages))
	for _, message := range messages {
		messageIDs = append(messageIDs, message.ID)
	}
	mentionsByMessage, err := s.svcCtx.Store.ListMessagesMentions(ctx, messageIDs)
	if err != nil {
		return nil, mapStoreError(err)
	}
	for _, message := range messages {
		if mentions, ok := mentionsByMessage[message.ID]; ok {
			message.Mentions.UserIDs = mentions.UserIDs
			message.Mentions.RoleIDs = mentions.RoleIDs
		}
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

func (s *messageServer) getAuthor(ctx context.Context, userID int64) (*userv1.UserProfile, error) {
	req := new(userv1.GetUserProfileRequest)
	req.SetUserId(userID)
	resp, err := s.svcCtx.UserClient.GetUserProfile(ctx, req)
	if err != nil {
		return nil, err
	}
	profile := resp.GetProfile()
	if profile == nil || profile.GetUserId() != userID {
		return nil, status.Error(codes.Internal, "user service returned an invalid profile")
	}
	return profile, nil
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
