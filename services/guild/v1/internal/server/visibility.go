package server

import (
	"context"
	"slices"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
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
	guildIDs := make([]int64, 0, len(guilds))
	for _, guild := range guilds {
		guildIDs = append(guildIDs, guild.ID)
	}
	roles, err := s.svcCtx.Store.ListGuildMemberRolesByGuilds(ctx, guildIDs, req.GetUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	channels, err := s.svcCtx.Store.ListGuildChannelsByGuilds(ctx, guildIDs)
	if err != nil {
		return nil, mapStoreError(err)
	}
	overwrites, err := s.svcCtx.Store.ListGuildChannelPermissionOverwritesByGuilds(ctx, guildIDs, req.GetUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	rolesByGuild := groupRolesByGuild(roles)
	channelsByGuild := groupChannelsByGuild(channels)
	overwritesByGuild := groupOverwritesByGuild(overwrites)

	visibilities := make([]*guildv1.GuildChannelVisibility, 0, len(guilds))
	for _, guild := range guilds {
		authority := memberAuthorityFromRoles(guild, rolesByGuild[guild.ID], req.GetUserId())
		visible := visibleGuildChannels(authority, rolesByGuild[guild.ID], channelsByGuild[guild.ID], overwritesByGuild[guild.ID], req.GetUserId())
		channelIDs := make([]int64, 0, len(visible))
		for _, channel := range visible {
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

	guild, err := s.svcCtx.Store.GetGuildForMember(ctx, req.GetGuildId(), req.GetUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	channels, err := s.svcCtx.Store.ListGuildChannels(ctx, req.GetGuildId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	var roles []*model.Role
	var overwrites []*model.ChannelPermissionOverwrite
	if guild.OwnerID != req.GetUserId() {
		roles, err = s.svcCtx.Store.ListGuildMemberRoles(ctx, req.GetGuildId(), req.GetUserId())
		if err != nil {
			return nil, mapStoreError(err)
		}
		overwrites, err = s.svcCtx.Store.ListGuildChannelPermissionOverwritesByGuild(ctx, req.GetGuildId())
		if err != nil {
			return nil, mapStoreError(err)
		}
	}
	channels = visibleGuildChannels(memberAuthorityFromRoles(guild, roles, req.GetUserId()), roles, channels, overwrites, req.GetUserId())

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

func groupRolesByGuild(roles []*model.Role) map[int64][]*model.Role {
	grouped := make(map[int64][]*model.Role)
	for _, role := range roles {
		grouped[role.GuildID] = append(grouped[role.GuildID], role)
	}
	return grouped
}

func groupChannelsByGuild(channels []*model.Channel) map[int64][]*model.Channel {
	grouped := make(map[int64][]*model.Channel)
	for _, channel := range channels {
		grouped[channel.GuildID] = append(grouped[channel.GuildID], channel)
	}
	return grouped
}

func groupOverwritesByGuild(overwrites []*model.ChannelPermissionOverwrite) map[int64][]*model.ChannelPermissionOverwrite {
	grouped := make(map[int64][]*model.ChannelPermissionOverwrite)
	for _, overwrite := range overwrites {
		grouped[overwrite.GuildID] = append(grouped[overwrite.GuildID], overwrite)
	}
	return grouped
}
