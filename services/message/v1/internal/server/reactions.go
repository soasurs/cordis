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
	if _, err := s.svcCtx.Store.GetMessage(ctx, req.GetMessageId()); err != nil {
		return nil, mapStoreError(err)
	}
	if err := s.svcCtx.Store.AddReaction(ctx, req.GetMessageId(), req.GetUserId(), req.GetEmojiId(), req.GetEmojiName()); err != nil {
		return nil, mapStoreError(err)
	}

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
	if _, err := s.svcCtx.Store.GetMessage(ctx, req.GetMessageId()); err != nil {
		return nil, mapStoreError(err)
	}
	if err := s.svcCtx.Store.RemoveReaction(ctx, req.GetMessageId(), req.GetUserId(), req.GetEmojiId(), req.GetEmojiName()); err != nil {
		return nil, mapStoreError(err)
	}

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
		nextCursor = userIDs[limit-1]
		userIDs = userIDs[:limit]
	}

	resp := new(messagev1.ListReactionUsersResponse)
	resp.SetUserIds(userIDs)
	resp.SetNextCursor(nextCursor)
	return resp, nil
}
