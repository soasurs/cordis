package server

import (
	"context"
	"slices"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

func (s *guildServer) ListUserGuildChannelVisibilities(
	ctx context.Context,
	req *guildv1.ListUserGuildChannelVisibilitiesRequest,
) (*guildv1.ListUserGuildChannelVisibilitiesResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	if req.GetBeforeGuildId() < 0 {
		return nil, invalidRequest("before guild id must not be negative")
	}
	limit, err := normalizeLimit(req.GetLimit())
	if err != nil {
		return nil, err
	}
	guilds, err := s.svcCtx.Store.ListUserGuilds(ctx, store.ListUserGuildsParams{
		UserID: req.GetUserId(), Before: req.GetBeforeGuildId(), Limit: limit,
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	visibilities := make([]*guildv1.GuildChannelVisibility, 0, len(guilds))
	for _, guild := range guilds {
		channels, err := loadVisibleGuildChannels(ctx, s.svcCtx.Store, guild.ID, req.GetUserId())
		if err != nil {
			return nil, mapStoreError(err)
		}
		channelIDs := make([]int64, 0, len(channels))
		for _, channel := range channels {
			channelIDs = append(channelIDs, channel.ID)
		}
		slices.Sort(channelIDs)
		visibility := new(guildv1.GuildChannelVisibility)
		visibility.SetGuildId(guild.ID)
		visibility.SetAccessRevision(guild.AccessRevision)
		visibility.SetVisibleChannelIds(channelIDs)
		visibilities = append(visibilities, visibility)
	}

	resp := new(guildv1.ListUserGuildChannelVisibilitiesResponse)
	resp.SetVisibilities(visibilities)
	if len(guilds) > 0 {
		resp.SetBeforeGuildId(guilds[len(guilds)-1].ID)
	}
	return resp, nil
}

func (s *guildServer) GetUserGuildChannelVisibility(
	ctx context.Context,
	req *guildv1.GetUserGuildChannelVisibilityRequest,
) (*guildv1.GetUserGuildChannelVisibilityResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	if req.GetGuildId() <= 0 {
		return nil, invalidRequest("guild id is required")
	}

	guild, err := s.svcCtx.Store.GetGuild(ctx, req.GetGuildId())
	if err != nil {
		return nil, mapStoreError(err)
	}

	channels, err := loadVisibleGuildChannels(ctx, s.svcCtx.Store, req.GetGuildId(), req.GetUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}

	channelIDs := make([]int64, 0, len(channels))
	for _, channel := range channels {
		channelIDs = append(channelIDs, channel.ID)
	}
	slices.Sort(channelIDs)

	visibility := new(guildv1.GuildChannelVisibility)
	visibility.SetGuildId(req.GetGuildId())
	visibility.SetAccessRevision(guild.AccessRevision)
	visibility.SetVisibleChannelIds(channelIDs)

	resp := new(guildv1.GetUserGuildChannelVisibilityResponse)
	resp.SetVisibility(visibility)
	return resp, nil
}
