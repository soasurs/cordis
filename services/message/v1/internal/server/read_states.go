package server

import (
	"context"

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

const (
	defaultLastReadID    = int64(0)
	maxReadStateChannels = 100
)

func (s *messageServer) AckMessage(ctx context.Context, req *messagev1.AckMessageRequest) (*messagev1.AckMessageResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	if req.GetChannelId() <= 0 {
		return nil, invalidRequest("channel id is required")
	}
	if req.GetMessageId() <= 0 {
		return nil, invalidRequest("message id is required")
	}

	if err := s.requireChannelPermission(ctx, req.GetChannelId(), req.GetUserId(), permissionViewChannel); err != nil {
		return nil, err
	}

	if err := s.svcCtx.Store.AckMessage(ctx, req.GetUserId(), req.GetChannelId(), req.GetMessageId()); err != nil {
		return nil, err
	}

	resp := new(messagev1.AckMessageResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *messageServer) GetReadStates(ctx context.Context, req *messagev1.GetReadStatesRequest) (*messagev1.GetReadStatesResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	channelIDs, err := normalizeReadStateChannelIDs(req.GetChannelIds())
	if err != nil {
		return nil, err
	}
	for _, channelID := range channelIDs {
		if err := s.requireChannelPermission(ctx, channelID, req.GetUserId(), permissionViewChannel); err != nil {
			return nil, err
		}
	}

	states, err := s.svcCtx.Store.ListChannelReadStates(ctx, req.GetUserId(), channelIDs)
	if err != nil {
		return nil, mapStoreError(err)
	}

	stateByChannel := make(map[int64]*model.ChannelReadState, len(states))
	for _, st := range states {
		stateByChannel[st.ChannelID] = st
	}

	var pbStates []*messagev1.ChannelReadState
	for _, channelID := range channelIDs {
		st, ok := stateByChannel[channelID]
		if !ok {
			// No read state means the user has never acked: count from zero.
			st = &model.ChannelReadState{ChannelID: channelID}
		}

		missing, err := s.svcCtx.Store.CountMissingMessages(ctx, channelID, st.LastReadMessageID, req.GetUserId())
		if err != nil {
			return nil, err
		}
		st.MessageCount = missing

		mentions, err := s.svcCtx.Store.CountUnreadMentions(ctx, req.GetUserId(), channelID, st.LastReadMessageID)
		if err != nil {
			return nil, err
		}
		st.MentionCount = mentions

		pb := new(messagev1.ChannelReadState)
		pb.SetChannelId(channelID)
		pb.SetLastReadMessageId(st.LastReadMessageID)
		pb.SetMentionCount(st.MentionCount)
		pb.SetMissingMessageCount(st.MessageCount)
		pbStates = append(pbStates, pb)
	}

	resp := new(messagev1.GetReadStatesResponse)
	resp.SetStates(pbStates)
	return resp, nil
}

func normalizeReadStateChannelIDs(channelIDs []int64) ([]int64, error) {
	if len(channelIDs) > maxReadStateChannels {
		return nil, invalidRequest("too many channel ids")
	}
	unique := make([]int64, 0, len(channelIDs))
	seen := make(map[int64]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID <= 0 {
			return nil, invalidRequest("channel id is required")
		}
		if _, ok := seen[channelID]; ok {
			continue
		}
		seen[channelID] = struct{}{}
		unique = append(unique, channelID)
	}
	return unique, nil
}
