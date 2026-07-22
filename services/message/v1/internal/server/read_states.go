package server

import (
	"context"
	"errors"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
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

	if _, err := s.requireChannelPermission(ctx, req.GetChannelId(), req.GetUserId(), permissionViewChannel); err != nil {
		return nil, err
	}

	var advanced bool
	var state *model.ChannelReadState
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		var err error
		advanced, err = txStore.AckMessage(ctx, req.GetUserId(), req.GetChannelId(), req.GetMessageId())
		if err != nil {
			return err
		}
		states, err := txStore.ListReadyChannelReadStates(ctx, req.GetUserId(), []int64{req.GetChannelId()})
		if err != nil {
			return err
		}
		if len(states) != 1 {
			return notFound()
		}
		state = states[0]
		return nil
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	if advanced {
		s.publishReadStateUpdated(ctx, state)
	}

	resp := new(messagev1.AckMessageResponse)
	resp.SetReadState(channelReadStateToProto(state))
	return resp, nil
}

func (s *messageServer) GetUserReadyState(ctx context.Context, req *messagev1.GetUserReadyStateRequest) (*messagev1.GetUserReadyStateResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	channelIDs, seen, err := normalizeReadyChannelIDs(req.GetGuildChannelIds())
	if err != nil {
		return nil, err
	}
	dmChannels, err := s.svcCtx.Store.ListAllDmChannels(ctx, req.GetUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	for _, channel := range dmChannels {
		if _, ok := seen[channel.ID]; ok {
			continue
		}
		seen[channel.ID] = struct{}{}
		channelIDs = append(channelIDs, channel.ID)
	}
	states, err := s.listReadyChannelReadStates(ctx, req.GetUserId(), channelIDs)
	if err != nil {
		return nil, err
	}
	pbStates := make([]*messagev1.ChannelReadState, 0, len(states))
	for _, st := range states {
		pbStates = append(pbStates, channelReadStateToProto(st))
	}

	resp := new(messagev1.GetUserReadyStateResponse)
	values := make([]*messagev1.DmChannel, 0, len(dmChannels))
	for _, channel := range dmChannels {
		values = append(values, dmChannelToProto(channel))
	}
	resp.SetDmChannels(values)
	resp.SetReadStates(pbStates)
	return resp, nil
}

func (s *messageServer) GetReadStates(ctx context.Context, req *messagev1.GetReadStatesRequest) (*messagev1.GetReadStatesResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	var channelIDs []int64
	var dmChannels []*model.DmChannel
	switch req.GetScope() {
	case messagev1.ReadStateScopeType_READ_STATE_SCOPE_TYPE_GUILD:
		if req.GetGuildId() <= 0 {
			return nil, invalidRequest("guild id is required")
		}
		visibilityReq := new(guildv1.GetUserGuildChannelVisibilityRequest)
		visibilityReq.SetUserId(req.GetUserId())
		visibilityReq.SetGuildId(req.GetGuildId())
		visibilityResp, err := s.svcCtx.GuildClient.GetUserGuildChannelVisibility(ctx, visibilityReq)
		if err != nil {
			return nil, err
		}
		visibility := visibilityResp.GetVisibility()
		if visibility == nil || visibility.GetGuildId() != req.GetGuildId() || visibility.GetAccessRevision() <= 0 {
			return nil, status.Error(codes.Internal, "guild visibility response is invalid")
		}
		channelIDs = visibility.GetVisibleTextChannelIds()
	case messagev1.ReadStateScopeType_READ_STATE_SCOPE_TYPE_ALL_DMS:
		if req.GetGuildId() != 0 {
			return nil, invalidRequest("guild id is not valid for dm scope")
		}
		var err error
		dmChannels, err = s.svcCtx.Store.ListAllDmChannels(ctx, req.GetUserId())
		if err != nil {
			return nil, mapStoreError(err)
		}
		channelIDs = make([]int64, 0, len(dmChannels))
		for _, channel := range dmChannels {
			channelIDs = append(channelIDs, channel.ID)
		}
	default:
		return nil, invalidRequest("read state scope is required")
	}
	states, err := s.listReadyChannelReadStates(ctx, req.GetUserId(), channelIDs)
	if err != nil {
		return nil, err
	}
	resp := new(messagev1.GetReadStatesResponse)
	resp.SetDmChannels(dmChannelsToProto(dmChannels))
	resp.SetReadStates(channelReadStatesToProto(states))
	return resp, nil
}

func (s *messageServer) listReadyChannelReadStates(
	ctx context.Context,
	userID int64,
	channelIDs []int64,
) ([]*model.ChannelReadState, error) {
	if len(channelIDs) == 0 {
		return nil, nil
	}
	if s.svcCtx.ReadStatesLimiter == nil {
		states, err := s.svcCtx.Store.ListReadyChannelReadStates(ctx, userID, channelIDs)
		if err != nil {
			return nil, mapStoreError(err)
		}
		return states, nil
	}

	capacity := s.svcCtx.Cfg.ReadStates.MaxConcurrentChannels
	if capacity <= 0 {
		return nil, status.Error(codes.Internal, "read states concurrency capacity is invalid")
	}
	states := make([]*model.ChannelReadState, 0, len(channelIDs))
	for len(channelIDs) > 0 {
		batchSize := len(channelIDs)
		if int64(batchSize) > capacity {
			batchSize = int(capacity)
		}
		batch := channelIDs[:batchSize]
		release, err := s.acquireReadStatesCapacity(ctx, int64(batchSize))
		if err != nil {
			return nil, err
		}
		batchStates, queryErr := s.svcCtx.Store.ListReadyChannelReadStates(ctx, userID, batch)
		release()
		if queryErr != nil {
			return nil, mapStoreError(queryErr)
		}
		states = append(states, batchStates...)
		channelIDs = channelIDs[batchSize:]
	}
	return states, nil
}

func (s *messageServer) acquireReadStatesCapacity(ctx context.Context, weight int64) (func(), error) {
	if s.svcCtx.ReadStatesLimiter == nil {
		return func() {}, nil
	}
	release, err := s.svcCtx.ReadStatesLimiter.Acquire(ctx, weight)
	if err == nil {
		return release, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, status.FromContextError(err).Err()
	}
	logx.WithContext(ctx).Errorw("acquire read states capacity", logx.Field("error", err))
	return nil, status.Error(codes.Internal, "read states concurrency limiter unavailable")
}

func normalizeReadyChannelIDs(channelIDs []int64) ([]int64, map[int64]struct{}, error) {
	unique := make([]int64, 0, len(channelIDs))
	seen := make(map[int64]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID <= 0 {
			return nil, nil, invalidRequest("channel id is required")
		}
		if _, ok := seen[channelID]; ok {
			continue
		}
		seen[channelID] = struct{}{}
		unique = append(unique, channelID)
	}
	return unique, seen, nil
}

func channelReadStateToProto(state *model.ChannelReadState) *messagev1.ChannelReadState {
	pb := new(messagev1.ChannelReadState)
	pb.SetChannelId(state.ChannelID)
	pb.SetLastMessageId(state.LastMessageID)
	pb.SetLastReadMessageId(state.LastReadMessageID)
	pb.SetMentionCount(state.MentionCount)
	return pb
}

func channelReadStatesToProto(states []*model.ChannelReadState) []*messagev1.ChannelReadState {
	values := make([]*messagev1.ChannelReadState, 0, len(states))
	for _, state := range states {
		values = append(values, channelReadStateToProto(state))
	}
	return values
}

func dmChannelsToProto(channels []*model.DmChannel) []*messagev1.DmChannel {
	values := make([]*messagev1.DmChannel, 0, len(channels))
	for _, channel := range channels {
		values = append(values, dmChannelToProto(channel))
	}
	return values
}

func (s *messageServer) publishReadStateUpdated(ctx context.Context, state *model.ChannelReadState) {
	event, err := newMessageReadUpdatedEvent(state, s.svcCtx.Snowflake.Generate().Int64())
	s.publishEvent(ctx, event, err)
}
