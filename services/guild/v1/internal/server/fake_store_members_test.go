package server

import (
	"context"
	"database/sql"
	"slices"
	"sort"
	"strings"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

func (s *fakeStore) CreateGuildMember(_ context.Context, guildID, userID, joinedAt int64) (*model.GuildMember, error) {
	if s.members[guildID] == nil {
		s.members[guildID] = make(map[int64]*model.GuildMember)
	}
	if existing := s.members[guildID][userID]; existing != nil && existing.DeletedAt == 0 {
		return nil, store.ErrMemberAlreadyExists
	}
	revision := int64(1)
	if existing := s.members[guildID][userID]; existing != nil {
		revision = existing.Revision + 1
	}
	member := &model.GuildMember{
		GuildID: guildID, UserID: userID, Revision: revision, JoinedAt: joinedAt,
	}
	s.members[guildID][userID] = member
	return cloneMember(member), nil
}

func (s *fakeStore) GetGuildMember(_ context.Context, guildID, userID int64) (*model.GuildMember, error) {
	member := s.members[guildID][userID]
	if member == nil || member.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	return cloneMember(member), nil
}

func (s *fakeStore) ListGuildMembers(_ context.Context, params store.ListGuildMembersParams) ([]*model.GuildMember, error) {
	var members []*model.GuildMember
	for _, member := range s.members[params.GuildID] {
		if member.DeletedAt != 0 {
			continue
		}
		if params.BeforeJoinedAt != 0 {
			if member.JoinedAt > params.BeforeJoinedAt ||
				(member.JoinedAt == params.BeforeJoinedAt && member.UserID >= params.BeforeUserID) {
				continue
			}
		}
		members = append(members, cloneMember(member))
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].JoinedAt != members[j].JoinedAt {
			return members[i].JoinedAt > members[j].JoinedAt
		}
		return members[i].UserID > members[j].UserID
	})
	if len(members) > params.Limit {
		members = members[:params.Limit]
	}
	return members, nil
}

func (s *fakeStore) ListGuildMemberIDsPage(_ context.Context, guildID, afterUserID int64, limit int) ([]int64, error) {
	var userIDs []int64
	for _, member := range s.members[guildID] {
		if member.DeletedAt != 0 || member.UserID <= afterUserID {
			continue
		}
		userIDs = append(userIDs, member.UserID)
	}
	slices.Sort(userIDs)
	if len(userIDs) > limit {
		userIDs = userIDs[:limit]
	}
	return userIDs, nil
}

func (s *fakeStore) ListGuildRoleTargetIDsPage(_ context.Context, guildID int64, roleIDs []int64, afterUserID int64, limit int) ([]int64, error) {
	roleSet := make(map[int64]struct{}, len(roleIDs))
	for _, roleID := range roleIDs {
		roleSet[roleID] = struct{}{}
	}
	var userIDs []int64
	for userID, assignments := range s.memberRoles[guildID] {
		member := s.members[guildID][userID]
		if member == nil || member.DeletedAt != 0 || userID <= afterUserID {
			continue
		}
		for roleID := range assignments {
			if _, ok := roleSet[roleID]; ok {
				userIDs = append(userIDs, userID)
				break
			}
		}
	}
	slices.Sort(userIDs)
	if len(userIDs) > limit {
		userIDs = userIDs[:limit]
	}
	return userIDs, nil
}

func (s *fakeStore) ListGuildMemberRolesByUsers(ctx context.Context, guildID int64, userIDs []int64) (map[int64][]*model.Role, error) {
	byUser := make(map[int64][]*model.Role, len(userIDs))
	for _, userID := range userIDs {
		member := s.members[guildID][userID]
		if member == nil || member.DeletedAt != 0 {
			continue
		}
		roles, err := s.ListGuildMemberRoles(ctx, guildID, userID)
		if err != nil {
			return nil, err
		}
		byUser[userID] = roles
	}
	return byUser, nil
}

func (s *fakeStore) SearchGuildMentionUsers(_ context.Context, params store.SearchGuildMentionUsersParams) ([]*model.GuildMemberProfile, error) {
	query := strings.ToLower(strings.TrimSpace(params.Query))
	type result struct {
		profile *model.GuildMemberProfile
		rank    int32
	}
	var results []result
	for userID, member := range s.members[params.GuildID] {
		if member == nil || member.DeletedAt != 0 {
			continue
		}
		profile := s.profiles[params.GuildID][userID]
		if profile == nil {
			continue
		}
		username := strings.ToLower(profile.Username)
		name := strings.ToLower(profile.Name)
		nickname := strings.ToLower(profile.Nickname)
		rank := int32(-1)
		if strings.HasPrefix(username, query) {
			rank = 0
		} else if strings.HasPrefix(nickname, query) || strings.HasPrefix(name, query) {
			rank = 1
		}
		if rank < 0 {
			continue
		}
		if params.After && (rank < params.AfterMatchRank || (rank == params.AfterMatchRank &&
			(username < params.AfterUsername || (username == params.AfterUsername && userID <= params.AfterUserID)))) {
			continue
		}
		copy := *profile
		results = append(results, result{profile: &copy, rank: rank})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].rank != results[j].rank {
			return results[i].rank < results[j].rank
		}
		left := strings.ToLower(results[i].profile.Username)
		right := strings.ToLower(results[j].profile.Username)
		if left != right {
			return left < right
		}
		return results[i].profile.UserID < results[j].profile.UserID
	})
	profiles := make([]*model.GuildMemberProfile, 0, min(params.Limit, len(results)))
	for _, value := range results {
		profiles = append(profiles, value.profile)
		if len(profiles) == params.Limit {
			break
		}
	}
	return profiles, nil
}

func (s *fakeStore) UpsertGuildMemberProfile(_ context.Context, profile *model.GuildMemberProfile) error {
	if profile == nil {
		return nil
	}
	if s.profiles[profile.GuildID] == nil {
		s.profiles[profile.GuildID] = make(map[int64]*model.GuildMemberProfile)
	}
	if current := s.profiles[profile.GuildID][profile.UserID]; current != nil && current.ProfileUpdatedAt > profile.ProfileUpdatedAt {
		return nil
	}
	copy := *profile
	s.profiles[profile.GuildID][profile.UserID] = &copy
	return nil
}

func (s *fakeStore) UpdateGuildMemberProfilesByUser(_ context.Context, profile *model.GuildMemberProfile) error {
	if profile == nil {
		return nil
	}
	for guildID, profiles := range s.profiles {
		current := profiles[profile.UserID]
		if current == nil || current.ProfileUpdatedAt > profile.ProfileUpdatedAt {
			continue
		}
		copy := *profile
		copy.GuildID = guildID
		copy.Nickname = current.Nickname
		copy.NicknameSearch = current.NicknameSearch
		profiles[profile.UserID] = &copy
	}
	return nil
}

func (s *fakeStore) UpdateGuildMemberProfilesByUserWithoutAvatar(_ context.Context, profile *model.GuildMemberProfile) error {
	if profile == nil {
		return nil
	}
	for guildID, profiles := range s.profiles {
		current := profiles[profile.UserID]
		if current == nil || current.ProfileUpdatedAt > profile.ProfileUpdatedAt {
			continue
		}
		copy := *current
		copy.Username = profile.Username
		copy.Name = profile.Name
		copy.UsernameSearch = strings.ToLower(strings.TrimSpace(profile.Username))
		copy.NameSearch = strings.ToLower(strings.TrimSpace(profile.Name))
		copy.ProfileUpdatedAt = profile.ProfileUpdatedAt
		copy.GuildID = guildID
		profiles[profile.UserID] = &copy
	}
	return nil
}

func (s *fakeStore) UpdateGuildMemberProfileNickname(_ context.Context, guildID, userID int64, nickname string) error {
	profile := s.profiles[guildID][userID]
	if profile == nil {
		return nil
	}
	profile.Nickname = nickname
	profile.NicknameSearch = strings.ToLower(strings.TrimSpace(nickname))
	return nil
}

func (s *fakeStore) DeleteGuildMemberProfile(_ context.Context, guildID, userID int64) error {
	delete(s.profiles[guildID], userID)
	return nil
}

func (s *fakeStore) DeleteGuildMemberProfiles(_ context.Context, guildID int64) error {
	s.profiles[guildID] = nil
	return nil
}

func (s *fakeStore) ListGuildMemberProfileKeys(_ context.Context, params store.ListGuildMemberProfileKeysParams) ([]model.GuildMemberProfileKey, error) {
	var keys []model.GuildMemberProfileKey
	for guildID, members := range s.members {
		for userID, member := range members {
			if member == nil || member.DeletedAt != 0 {
				continue
			}
			if params.AfterGuildID != 0 && (guildID < params.AfterGuildID || (guildID == params.AfterGuildID && userID <= params.AfterUserID)) {
				continue
			}
			keys = append(keys, model.GuildMemberProfileKey{GuildID: guildID, UserID: userID, Nickname: member.Nickname})
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].GuildID != keys[j].GuildID {
			return keys[i].GuildID < keys[j].GuildID
		}
		return keys[i].UserID < keys[j].UserID
	})
	if len(keys) > params.Limit {
		keys = keys[:params.Limit]
	}
	return keys, nil
}

func (s *fakeStore) ListUsersWithCommonGuild(_ context.Context, userID int64, targetUserIDs []int64) ([]int64, error) {
	actorGuilds := make(map[int64]struct{})
	for guildID, members := range s.members {
		if members[userID] != nil && members[userID].DeletedAt == 0 {
			actorGuilds[guildID] = struct{}{}
		}
	}
	var result []int64
	for _, targetUserID := range targetUserIDs {
		for guildID := range actorGuilds {
			if member := s.members[guildID][targetUserID]; member != nil && member.DeletedAt == 0 {
				result = append(result, targetUserID)
				break
			}
		}
	}
	return result, nil
}

func (s *fakeStore) ListGuildRoleMembers(_ context.Context, params store.ListGuildRoleMembersParams) ([]*model.GuildMember, error) {
	role := s.roles[params.GuildID][params.RoleID]
	if role == nil || role.DeletedAt != 0 {
		return nil, nil
	}
	var members []*model.GuildMember
	for userID, member := range s.members[params.GuildID] {
		if member.DeletedAt != 0 {
			continue
		}
		if params.BeforeJoinedAt != 0 {
			if member.JoinedAt > params.BeforeJoinedAt ||
				(member.JoinedAt == params.BeforeJoinedAt && member.UserID >= params.BeforeUserID) {
				continue
			}
		}
		if !role.IsDefault && !s.memberRoles[params.GuildID][userID][params.RoleID] {
			continue
		}
		members = append(members, cloneMember(member))
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].JoinedAt != members[j].JoinedAt {
			return members[i].JoinedAt > members[j].JoinedAt
		}
		return members[i].UserID > members[j].UserID
	})
	if len(members) > params.Limit {
		members = members[:params.Limit]
	}
	return members, nil
}

func (s *fakeStore) UpdateGuildMemberNickname(_ context.Context, guildID, userID int64, nickname string) (*model.GuildMember, error) {
	member := s.members[guildID][userID]
	if member == nil || member.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	member.Nickname = nickname
	member.Revision++
	member.UpdatedAt = 2
	return cloneMember(member), nil
}

func (s *fakeStore) RemoveGuildMember(_ context.Context, guildID, userID, removedAt int64) (*model.GuildMember, error) {
	member := s.members[guildID][userID]
	if member == nil || member.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	member.Revision++
	member.UpdatedAt = removedAt
	member.DeletedAt = removedAt
	return cloneMember(member), nil
}
