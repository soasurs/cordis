package server

import (
	"context"
	"errors"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
)

const (
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

	if _, err := s.requireChannelPermission(ctx, req.GetChannelId(), req.GetUserId(), permissionViewChannel); err != nil {
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
	release, err := s.acquireReadStatesCapacity(ctx, int64(max(1, len(channelIDs))))
	if err != nil {
		return nil, err
	}
	defer release()

	if err := s.authorizeReadStateChannels(ctx, req.GetUserId(), channelIDs); err != nil {
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

func (s *messageServer) authorizeReadStateChannels(ctx context.Context, userID int64, channelIDs []int64) error {
	dmChannels, err := s.svcCtx.Store.ListDmChannelsByIDs(ctx, channelIDs)
	if err != nil {
		return mapStoreError(err)
	}
	dmByID := make(map[int64]struct{}, len(dmChannels))
	for _, channel := range dmChannels {
		if err := s.authorizeDmMessage(ctx, channel, userID, permissionViewChannel); err != nil {
			return err
		}
		dmByID[channel.ID] = struct{}{}
	}
	guildChannelIDs := make([]int64, 0, len(channelIDs)-len(dmChannels))
	for _, channelID := range channelIDs {
		if _, ok := dmByID[channelID]; !ok {
			guildChannelIDs = append(guildChannelIDs, channelID)
		}
	}
	if len(guildChannelIDs) == 0 {
		return nil
	}
	req := new(guildv1.AuthorizeGuildChannelsRequest)
	req.SetUserId(userID)
	req.SetChannelIds(guildChannelIDs)
	req.SetPermission(permissionViewChannel)
	resp, err := s.svcCtx.GuildClient.AuthorizeGuildChannels(ctx, req)
	if err != nil {
		return err
	}
	if len(resp.GetAuthorizations()) != len(guildChannelIDs) {
		return errors.New("guild channel authorization returned incomplete batch")
	}
	expected := make(map[int64]struct{}, len(guildChannelIDs))
	for _, channelID := range guildChannelIDs {
		expected[channelID] = struct{}{}
	}
	for _, authorization := range resp.GetAuthorizations() {
		if _, ok := expected[authorization.GetChannelId()]; !ok {
			return errors.New("guild channel authorization returned unexpected channel")
		}
		delete(expected, authorization.GetChannelId())
		if !authorization.GetAllowed() {
			return permissionDenied()
		}
		if authorization.GetChannelType() == guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_CATEGORY ||
			authorization.GetChannelType() == guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_VOICE {
			return invalidRequest("messages are only supported in text channels")
		}
	}
	if len(expected) != 0 {
		return errors.New("guild channel authorization returned incomplete batch")
	}
	return nil
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
