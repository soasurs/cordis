package server

import (
	"context"

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
)

func (s *messageServer) AddReaction(ctx context.Context, req *messagev1.AddReactionRequest) (*messagev1.AddReactionResponse, error) {
	if req.GetMessageId() <= 0 {
		return nil, invalidRequest("message id is required")
	}
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	if err := validateEmoji(req.GetEmojiId(), req.GetEmojiName()); err != nil {
		return nil, err
	}

	var channelID int64
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		msg, err := txStore.GetMessage(ctx, req.GetMessageId())
		if err != nil {
			return err
		}
		channelID = msg.ChannelID

		if err := txStore.AddReaction(ctx, req.GetMessageId(), req.GetUserId(), req.GetEmojiId(), req.GetEmojiName()); err != nil {
			return err
		}

		eventID := s.svcCtx.Snowflake.Generate().Int64()
		outboxEvent, err := newReactionEvent(
			s.svcCtx.Cfg.Kafka.Topic,
			eventID,
			EventTypeReactionAdded,
			s.svcCtx.OutboxMaxRetries,
			s.svcCtx.OutboxPartitionCount,
			req.GetMessageId(),
			channelID,
			req.GetUserId(),
			req.GetEmojiId(),
			req.GetEmojiName(),
		)
		if err != nil {
			return err
		}
		return txStore.InsertOutboxEvent(ctx, outboxEvent)
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	s.svcCtx.Relay.Notify()

	resp := new(messagev1.AddReactionResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *messageServer) RemoveReaction(ctx context.Context, req *messagev1.RemoveReactionRequest) (*messagev1.RemoveReactionResponse, error) {
	if req.GetMessageId() <= 0 {
		return nil, invalidRequest("message id is required")
	}
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	if err := validateEmoji(req.GetEmojiId(), req.GetEmojiName()); err != nil {
		return nil, err
	}

	var channelID int64
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		msg, err := txStore.GetMessage(ctx, req.GetMessageId())
		if err != nil {
			return err
		}
		channelID = msg.ChannelID

		if err := txStore.RemoveReaction(ctx, req.GetMessageId(), req.GetUserId(), req.GetEmojiId(), req.GetEmojiName()); err != nil {
			return err
		}

		eventID := s.svcCtx.Snowflake.Generate().Int64()
		outboxEvent, err := newReactionEvent(
			s.svcCtx.Cfg.Kafka.Topic,
			eventID,
			EventTypeReactionRemoved,
			s.svcCtx.OutboxMaxRetries,
			s.svcCtx.OutboxPartitionCount,
			req.GetMessageId(),
			channelID,
			req.GetUserId(),
			req.GetEmojiId(),
			req.GetEmojiName(),
		)
		if err != nil {
			return err
		}
		return txStore.InsertOutboxEvent(ctx, outboxEvent)
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	s.svcCtx.Relay.Notify()

	resp := new(messagev1.RemoveReactionResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *messageServer) ListReactionUsers(ctx context.Context, req *messagev1.ListReactionUsersRequest) (*messagev1.ListReactionUsersResponse, error) {
	if req.GetMessageId() <= 0 {
		return nil, invalidRequest("message id is required")
	}
	if err := validateEmoji(req.GetEmojiId(), req.GetEmojiName()); err != nil {
		return nil, err
	}
	if req.GetCursor() < 0 {
		return nil, invalidRequest("cursor must not be negative")
	}
	limit, err := normalizeLimit(req.GetLimit(), defaultReactionUserLimit, maxReactionUserLimit)
	if err != nil {
		return nil, err
	}
	if _, err := s.svcCtx.Store.GetMessage(ctx, req.GetMessageId()); err != nil {
		return nil, mapStoreError(err)
	}

	userIDs, err := s.svcCtx.Store.ListReactionUsers(ctx, store.ReactionKey{
		MessageID: req.GetMessageId(),
		EmojiID:   req.GetEmojiId(),
		EmojiName: req.GetEmojiName(),
	}, req.GetCursor(), limit+1)
	if err != nil {
		return nil, err
	}

	var nextCursor int64
	if len(userIDs) > limit {
		nextCursor = userIDs[limit]
		userIDs = userIDs[:limit]
	}

	resp := new(messagev1.ListReactionUsersResponse)
	resp.SetUserIds(userIDs)
	resp.SetNextCursor(nextCursor)
	return resp, nil
}
