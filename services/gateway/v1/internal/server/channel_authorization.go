package server

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
)

const permissionViewChannel = uint64(guildv1.GuildPermission_GUILD_PERMISSION_VIEW_CHANNEL)

func (s *Server) authorizeChannelSubscription(ctx context.Context, userID, channelID int64) (bool, error) {
	req := new(guildv1.AuthorizeGuildChannelRequest)
	req.SetUserId(userID)
	req.SetChannelId(channelID)
	req.SetPermission(permissionViewChannel)
	resp, err := s.svcCtx.GuildClient.AuthorizeGuildChannel(ctx, req)
	if err != nil {
		return false, err
	}
	return resp.GetAllowed(), nil
}

func (s *Server) authorizeChannelSubscriptions(ctx context.Context, userID int64, channelIDs []int64) ([]int64, error) {
	channelIDs, err := normalizeChannelIDs(channelIDs)
	if err != nil {
		return nil, err
	}
	for _, channelID := range channelIDs {
		allowed, err := s.authorizeChannelSubscription(ctx, userID, channelID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, errors.New("channel subscription permission denied")
		}
	}
	return channelIDs, nil
}

func normalizeChannelIDs(channelIDs []int64) ([]int64, error) {
	seen := make(map[int64]struct{}, len(channelIDs))
	normalized := make([]int64, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID <= 0 {
			return nil, errors.New("channel id must be positive")
		}
		if _, exists := seen[channelID]; exists {
			continue
		}
		seen[channelID] = struct{}{}
		normalized = append(normalized, channelID)
	}
	if len(normalized) == 0 {
		return nil, errors.New("channel ids are required")
	}
	return normalized, nil
}

func subscriptionInvalid(err error) bool {
	code := status.Code(err)
	return code == codes.NotFound || code == codes.PermissionDenied
}
