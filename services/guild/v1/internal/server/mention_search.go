package server

import (
	"context"
	"strings"
	"unicode/utf8"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

const (
	defaultMentionSearchLimit   = 20
	maxMentionSearchLimit       = 20
	mentionSearchCandidateBatch = 100
	maxMentionSearchQueryLength = 64
	// Message splits direct mention visibility requests to this size.
	maxVisibleUserFilterBatch = 100
)

func (s *guildServer) SearchGuildMentionUsers(ctx context.Context, req *guildv1.SearchGuildMentionUsersRequest) (*guildv1.SearchGuildMentionUsersResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if req.GetChannelId() <= 0 {
		return nil, invalidRequest("channel id is required")
	}
	query, err := normalizeMentionSearchQuery(req.GetQuery())
	if err != nil {
		return nil, err
	}
	limit, err := normalizeMentionSearchLimit(req.GetLimit())
	if err != nil {
		return nil, err
	}
	guild, overwrites, err := s.loadMentionChannelVisibility(ctx, req.GetGuildId(), req.GetActorUserId(), req.GetChannelId())
	if err != nil {
		return nil, err
	}

	var (
		after          bool
		afterMatchRank int32
		afterUsername  string
		afterUserID    int64
		users          []*guildv1.GuildMentionUser
	)
	for len(users) < limit {
		profiles, err := s.svcCtx.Store.SearchGuildMentionUsers(ctx, store.SearchGuildMentionUsersParams{
			GuildID:        req.GetGuildId(),
			Query:          query,
			After:          after,
			AfterMatchRank: afterMatchRank,
			AfterUsername:  afterUsername,
			AfterUserID:    afterUserID,
			Limit:          mentionSearchCandidateBatch,
		})
		if err != nil {
			return nil, mapStoreError(err)
		}
		if len(profiles) == 0 {
			break
		}
		userIDs := make([]int64, 0, len(profiles))
		for _, profile := range profiles {
			if profile != nil {
				userIDs = append(userIDs, profile.UserID)
			}
		}
		rolesByUser, err := s.svcCtx.Store.ListGuildMemberRolesByUsers(ctx, req.GetGuildId(), userIDs)
		if err != nil {
			return nil, mapStoreError(err)
		}
		for _, profile := range profiles {
			if profile == nil {
				continue
			}
			roles, ok := rolesByUser[profile.UserID]
			if !ok || !canViewMentionChannel(guild, overwrites, roles, profile.UserID) {
				continue
			}
			user := new(guildv1.GuildMentionUser)
			user.SetUserId(profile.UserID)
			user.SetUsername(profile.Username)
			user.SetName(profile.Name)
			user.SetAvatarAssetId(profile.AvatarAssetID)
			user.SetNickname(profile.Nickname)
			users = append(users, user)
			if len(users) == limit {
				break
			}
		}

		last := profiles[len(profiles)-1]
		if last == nil || len(profiles) < mentionSearchCandidateBatch {
			break
		}
		after = true
		afterMatchRank = mentionSearchMatchRank(last, query)
		afterUsername = mentionSearchUsername(last)
		afterUserID = last.UserID
	}

	resp := new(guildv1.SearchGuildMentionUsersResponse)
	resp.SetUsers(users)
	return resp, nil
}

func (s *guildServer) SearchGuildMentionRoles(ctx context.Context, req *guildv1.SearchGuildMentionRolesRequest) (*guildv1.SearchGuildMentionRolesResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	query, err := normalizeMentionSearchQuery(req.GetQuery())
	if err != nil {
		return nil, err
	}
	limit, err := normalizeMentionSearchLimit(req.GetLimit())
	if err != nil {
		return nil, err
	}
	if _, err := s.svcCtx.Store.GetGuildForMember(ctx, req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, mapStoreError(err)
	}
	roles, err := s.svcCtx.Store.ListGuildRoles(ctx, req.GetGuildId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	matched := make([]*model.Role, 0, limit)
	for _, role := range roles {
		if role == nil || role.IsDefault || !strings.HasPrefix(strings.ToLower(role.Name), query) {
			continue
		}
		matched = append(matched, role)
		if len(matched) == limit {
			break
		}
	}
	resp := new(guildv1.SearchGuildMentionRolesResponse)
	resp.SetRoles(guildRolesToProto(matched))
	return resp, nil
}

func (s *guildServer) FilterGuildChannelVisibleUsers(ctx context.Context, req *guildv1.FilterGuildChannelVisibleUsersRequest) (*guildv1.FilterGuildChannelVisibleUsersResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if req.GetChannelId() <= 0 {
		return nil, invalidRequest("channel id is required")
	}
	userIDs, err := normalizeVisibleUserIDs(req.GetUserIds())
	if err != nil {
		return nil, err
	}
	guild, overwrites, err := s.loadMentionChannelVisibility(ctx, req.GetGuildId(), req.GetActorUserId(), req.GetChannelId())
	if err != nil {
		return nil, err
	}
	if len(userIDs) == 0 {
		return new(guildv1.FilterGuildChannelVisibleUsersResponse), nil
	}
	rolesByUser, err := s.svcCtx.Store.ListGuildMemberRolesByUsers(ctx, req.GetGuildId(), userIDs)
	if err != nil {
		return nil, mapStoreError(err)
	}
	visible := make([]int64, 0, len(userIDs))
	for _, userID := range userIDs {
		roles, ok := rolesByUser[userID]
		if ok && canViewMentionChannel(guild, overwrites, roles, userID) {
			visible = append(visible, userID)
		}
	}
	resp := new(guildv1.FilterGuildChannelVisibleUsersResponse)
	resp.SetUserIds(visible)
	return resp, nil
}

func (s *guildServer) loadMentionChannelVisibility(ctx context.Context, guildID, actorUserID, channelID int64) (*model.Guild, []*model.ChannelPermissionOverwrite, error) {
	authority, roles, err := loadMemberAuthorityAndRoles(ctx, s.svcCtx.Store, guildID, actorUserID)
	if err != nil {
		return nil, nil, mapStoreError(err)
	}
	guild := authority.Guild
	channel, err := s.svcCtx.Store.GetGuildChannel(ctx, channelID)
	if err != nil {
		return nil, nil, mapStoreError(err)
	}
	if channel.GuildID != guildID {
		return nil, nil, invalidRequest("channel does not belong to guild")
	}
	overwrites, err := s.svcCtx.Store.ListGuildChannelPermissionOverwrites(ctx, channelID)
	if err != nil {
		return nil, nil, mapStoreError(err)
	}
	if !canViewMentionChannel(guild, overwrites, roles, actorUserID) {
		return nil, nil, permissionDenied()
	}
	return guild, overwrites, nil
}

func normalizeMentionSearchQuery(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", invalidRequest("search query is required")
	}
	if utf8.RuneCountInString(value) > maxMentionSearchQueryLength {
		return "", invalidRequest("search query is too long")
	}
	return value, nil
}

func normalizeMentionSearchLimit(value int32) (int, error) {
	if value < 0 || value > maxMentionSearchLimit {
		return 0, invalidRequest("search limit is out of range")
	}
	if value == 0 {
		return defaultMentionSearchLimit, nil
	}
	return int(value), nil
}

func normalizeVisibleUserIDs(values []int64) ([]int64, error) {
	if len(values) > maxVisibleUserFilterBatch {
		return nil, invalidRequest("too many user ids")
	}
	result := make([]int64, 0, len(values))
	seen := make(map[int64]struct{}, len(values))
	for _, userID := range values {
		if userID <= 0 {
			return nil, invalidRequest("user id must be positive")
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		result = append(result, userID)
	}
	return result, nil
}

func mentionSearchMatchRank(profile *model.GuildMemberProfile, query string) int32 {
	if strings.HasPrefix(mentionSearchUsername(profile), query) {
		return 0
	}
	return 1
}

func mentionSearchUsername(profile *model.GuildMemberProfile) string {
	if profile == nil {
		return ""
	}
	return profile.UsernameSearch
}
