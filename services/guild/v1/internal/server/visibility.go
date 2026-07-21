package server

import (
	"cmp"
	"context"
	"slices"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

func (s *guildServer) GetUserReadyState(
	ctx context.Context,
	req *guildv1.GetUserReadyStateRequest,
) (*guildv1.GetUserReadyStateResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	guilds, err := s.svcCtx.Store.ListUserGuilds(ctx, store.ListUserGuildsParams{
		UserID: req.GetUserId(), Limit: s.svcCtx.Cfg.Limits.JoinedGuilds(),
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	guildIDs := make([]int64, 0, len(guilds))
	for _, guild := range guilds {
		guildIDs = append(guildIDs, guild.ID)
	}
	roles, err := s.svcCtx.Store.ListGuildRolesByGuilds(ctx, guildIDs)
	if err != nil {
		return nil, mapStoreError(err)
	}
	memberRoles, err := s.svcCtx.Store.ListGuildMemberRolesByGuilds(ctx, guildIDs, req.GetUserId())
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
	memberRolesByGuild := groupRolesByGuild(memberRoles)
	channelsByGuild := groupChannelsByGuild(channels)
	overwritesByGuild := groupOverwritesByGuild(overwrites)

	readyGuilds := make([]*guildv1.ReadyGuild, 0, len(guilds))
	visibleChannelIDs := make([]int64, 0)
	for _, guild := range guilds {
		assignedRoles := memberRolesByGuild[guild.ID]
		authority := memberAuthorityFromRoles(guild, assignedRoles, req.GetUserId())
		visible := visibleGuildChannels(authority, assignedRoles, channelsByGuild[guild.ID], overwritesByGuild[guild.ID], req.GetUserId())
		slices.SortFunc(visible, func(a, b *model.Channel) int {
			if a.Position != b.Position {
				return cmp.Compare(a.Position, b.Position)
			}
			return cmp.Compare(a.ID, b.ID)
		})
		memberRoleIDs := make([]int64, 0, len(assignedRoles))
		for _, role := range assignedRoles {
			if !role.IsDefault {
				memberRoleIDs = append(memberRoleIDs, role.ID)
			}
		}
		slices.Sort(memberRoleIDs)
		ready := new(guildv1.ReadyGuild)
		ready.SetGuild(guildToProto(guild))
		ready.SetAccessRevision(guild.AccessRevision)
		ready.SetRoles(guildRolesToProto(rolesByGuild[guild.ID]))
		ready.SetMemberRoleIds(memberRoleIDs)
		ready.SetChannels(guildChannelsToProto(visible))
		for _, channel := range visible {
			visibleChannelIDs = append(visibleChannelIDs, channel.ID)
		}
		readyGuilds = append(readyGuilds, ready)
	}
	// The first overwrite query only loads targets that affect this user's
	// visibility. READY needs the complete overwrite metadata, so load all
	// overwrites only after the visible channel set has been reduced.
	visibleOverwrites, err := s.svcCtx.Store.ListGuildChannelPermissionOverwritesByChannels(ctx, visibleChannelIDs)
	if err != nil {
		return nil, mapStoreError(err)
	}
	visibleOverwritesByGuild := groupOverwritesByGuild(visibleOverwrites)
	for _, ready := range readyGuilds {
		ready.SetPermissionOverwrites(guildChannelOverwritesToProto(visibleOverwritesByGuild[ready.GetGuild().GetId()]))
	}

	resp := new(guildv1.GetUserReadyStateResponse)
	resp.SetGuilds(readyGuilds)
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
	textChannelIDs := make([]int64, 0, len(channels))
	for _, channel := range channels {
		channelIDs = append(channelIDs, channel.ID)
		if channel.Type == int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT) {
			textChannelIDs = append(textChannelIDs, channel.ID)
		}
	}
	slices.Sort(channelIDs)
	slices.Sort(textChannelIDs)

	visibility := new(guildv1.GuildChannelVisibility)
	visibility.SetGuildId(req.GetGuildId())
	visibility.SetAccessRevision(guild.AccessRevision)
	visibility.SetVisibleChannelIds(channelIDs)
	visibility.SetVisibleTextChannelIds(textChannelIDs)

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
