package server

import (
	"context"

	"golang.org/x/sync/errgroup"

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
)

const (
	maxReadStateChannels          = 100
	readStateAuthorizationWorkers = 8
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
		return nil, mapStoreError(err)
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
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(readStateAuthorizationWorkers)
	for _, channelID := range channelIDs {
		group.Go(func() error {
			return s.requireChannelPermission(groupCtx, channelID, req.GetUserId(), permissionViewChannel)
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}

	states, err := s.svcCtx.Store.ListChannelReadStatesWithCounts(ctx, req.GetUserId(), channelIDs)
	if err != nil {
		return nil, mapStoreError(err)
	}
	pbStates := make([]*messagev1.ChannelReadState, 0, len(states))
	for _, st := range states {
		pb := new(messagev1.ChannelReadState)
		pb.SetChannelId(st.ChannelID)
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
