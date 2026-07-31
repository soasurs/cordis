package server

import (
	"context"
	"slices"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

const (
	defaultMentionTargetsLimit = 100
	maxMentionTargetsLimit     = 1000
)

// ListGuildMentionTargets pages the active members a message would reach with
// the requested role and @everyone mentions, restricted to members who can
// view the channel.
func (s *guildServer) ListGuildMentionTargets(ctx context.Context, req *guildv1.ListGuildMentionTargetsRequest) (*guildv1.ListGuildMentionTargetsResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if req.GetChannelId() <= 0 {
		return nil, invalidRequest("channel id is required")
	}
	roleIDs, err := normalizeMentionRoleIDs(req.GetRoleIds())
	if err != nil {
		return nil, err
	}
	if !req.GetEveryone() && len(roleIDs) == 0 {
		return nil, invalidRequest("at least one mention source is required")
	}
	limit, err := normalizeMentionTargetsLimit(req.GetLimit())
	if err != nil {
		return nil, err
	}
	token, err := readCursor(req.HasCursor(), req.GetCursor())
	if err != nil {
		return nil, err
	}
	afterUserID, _, err := decodeMentionTargetsCursor(s.svcCtx.Cursors, token, req.GetGuildId(), req.GetChannelId(), roleIDs, req.GetEveryone())
	if err != nil {
		return nil, err
	}

	guild, err := s.svcCtx.Store.GetGuildForMember(ctx, req.GetGuildId(), req.GetActorUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	channel, err := s.svcCtx.Store.GetGuildChannel(ctx, req.GetChannelId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	if channel.GuildID != req.GetGuildId() {
		return nil, invalidRequest("channel does not belong to guild")
	}
	if len(roleIDs) > 0 {
		roles, err := s.svcCtx.Store.ListGuildRoles(ctx, req.GetGuildId())
		if err != nil {
			return nil, mapStoreError(err)
		}
		roleSet := make(map[int64]struct{}, len(roles))
		for _, role := range roles {
			roleSet[role.ID] = struct{}{}
		}
		for _, roleID := range roleIDs {
			if _, ok := roleSet[roleID]; !ok {
				return nil, invalidRequest("role does not belong to guild")
			}
		}
	}
	overwrites, err := s.svcCtx.Store.ListGuildChannelPermissionOverwrites(ctx, channel.ID)
	if err != nil {
		return nil, mapStoreError(err)
	}

	userIDs, hasMore, nextAfter, err := s.pageMentionTargets(ctx, req, guild, overwrites, roleIDs, afterUserID, limit)
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp := new(guildv1.ListGuildMentionTargetsResponse)
	resp.SetUserIds(userIDs)
	if err := setNextMentionTargetsCursor(s.svcCtx.Cursors, resp.SetNextCursor, hasMore, nextAfter, req.GetGuildId(), channel.ID, roleIDs, req.GetEveryone()); err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *guildServer) pageMentionTargets(
	ctx context.Context,
	req *guildv1.ListGuildMentionTargetsRequest,
	guild *model.Guild,
	overwrites []*model.ChannelPermissionOverwrite,
	roleIDs []int64,
	afterUserID int64,
	limit int,
) (userIDs []int64, hasMore bool, nextAfter int64, err error) {
	var candidates []int64
	if req.GetEveryone() {
		candidates, err = s.svcCtx.Store.ListGuildMemberIDsPage(ctx, req.GetGuildId(), afterUserID, limit+1)
	} else {
		candidates, err = s.svcCtx.Store.ListGuildRoleTargetIDsPage(ctx, req.GetGuildId(), roleIDs, afterUserID, limit+1)
	}
	if err != nil {
		return nil, false, 0, err
	}
	if len(candidates) == 0 {
		return nil, false, 0, nil
	}
	hasMore = len(candidates) > limit
	page := candidates
	if hasMore {
		page = candidates[:limit]
	}
	nextAfter = page[len(page)-1]

	rolesByUser, err := s.svcCtx.Store.ListGuildMemberRolesByUsers(ctx, req.GetGuildId(), page)
	if err != nil {
		return nil, false, 0, err
	}
	for _, userID := range page {
		if canViewMentionChannel(guild, overwrites, rolesByUser[userID], userID) {
			userIDs = append(userIDs, userID)
		}
	}
	return userIDs, hasMore, nextAfter, nil
}

// canViewMentionChannel reports whether userID can see the channel, reusing
// the single-user permission evaluation so batch results match per-user
// authorization exactly.
func canViewMentionChannel(guild *model.Guild, overwrites []*model.ChannelPermissionOverwrite, roles []*model.Role, userID int64) bool {
	authority := memberAuthorityFromRoles(guild, roles, userID)
	if authority.IsOwner || authority.Permissions&PermissionAdministrator != 0 {
		return true
	}
	permissions := channelPermissions(authority, roles, overwrites, userID)
	return permissions&PermissionViewChannel != 0
}

func normalizeMentionRoleIDs(roleIDs []int64) ([]int64, error) {
	values := make([]int64, 0, len(roleIDs))
	seen := make(map[int64]struct{}, len(roleIDs))
	for _, roleID := range roleIDs {
		if roleID <= 0 {
			return nil, invalidRequest("role id must be positive")
		}
		if _, ok := seen[roleID]; ok {
			return nil, invalidRequest("role ids must be unique")
		}
		seen[roleID] = struct{}{}
		values = append(values, roleID)
	}
	slices.Sort(values)
	return values, nil
}
