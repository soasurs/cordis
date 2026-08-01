package server

import (
	"context"
	"slices"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/services/message/v1/internal/mention"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

// Keep this in sync with Guild's maxVisibleUserFilterBatch and User's
// maxUserProfileBatch; both downstream RPCs reject larger requests.
const mentionUserBatchSize = 100

// resolveMentions parses content and reduces the mention set to entities the
// service is willing to persist. DM channels keep only user mentions; Guild
// channels require MENTION_EVERYONE for @everyone and drop roles and users
// that are no longer valid or cannot view the channel.
func (s *messageServer) resolveMentions(ctx context.Context, content string, audience messageAudience, actorUserID int64) (model.MessageMentions, error) {
	parsed := mention.Parse(content)
	if audience.guildID == 0 {
		parsed.RoleIDs = nil
		parsed.Everyone = false
		return s.filterMentionUsers(ctx, parsed, audience, actorUserID)
	}
	if parsed.Everyone && audience.permissions&permissionMentionEveryone == 0 {
		return model.MessageMentions{}, permissionDenied()
	}
	if len(parsed.RoleIDs) > 0 {
		roles, err := s.guildRoleSet(ctx, audience.guildID, actorUserID)
		if err != nil {
			return model.MessageMentions{}, err
		}
		kept := parsed.RoleIDs[:0]
		for _, roleID := range parsed.RoleIDs {
			if _, ok := roles[roleID]; ok {
				kept = append(kept, roleID)
			}
		}
		parsed.RoleIDs = kept
	}
	return s.filterMentionUsers(ctx, parsed, audience, actorUserID)
}

// filterMentionUsers drops user mentions whose profiles no longer exist and
// normalizes the resulting set to ascending ID order.
func (s *messageServer) filterMentionUsers(ctx context.Context, parsed mention.Set, audience messageAudience, actorUserID int64) (model.MessageMentions, error) {
	if len(parsed.UserIDs) > 0 {
		if audience.guildID != 0 {
			visible, err := s.filterGuildVisibleUsers(ctx, audience, actorUserID, parsed.UserIDs)
			if err != nil {
				return model.MessageMentions{}, err
			}
			parsed.UserIDs = visible
		}
		users, err := s.filterExistingUsers(ctx, parsed.UserIDs)
		if err != nil {
			return model.MessageMentions{}, err
		}
		parsed.UserIDs = users
	}
	return toMessageMentions(parsed), nil
}

func (s *messageServer) filterGuildVisibleUsers(ctx context.Context, audience messageAudience, actorUserID int64, userIDs []int64) ([]int64, error) {
	visible := make([]int64, 0, len(userIDs))
	for start := 0; start < len(userIDs); start += mentionUserBatchSize {
		end := min(start+mentionUserBatchSize, len(userIDs))
		req := new(guildv1.FilterGuildChannelVisibleUsersRequest)
		req.SetGuildId(audience.guildID)
		req.SetActorUserId(actorUserID)
		req.SetChannelId(audience.channelID)
		req.SetUserIds(userIDs[start:end])
		resp, err := s.svcCtx.GuildClient.FilterGuildChannelVisibleUsers(ctx, req)
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, status.Error(codes.Internal, "guild service returned an invalid visibility response")
		}
		visible = append(visible, resp.GetUserIds()...)
	}
	return visible, nil
}

func toMessageMentions(set mention.Set) model.MessageMentions {
	slices.Sort(set.UserIDs)
	slices.Sort(set.RoleIDs)
	return model.MessageMentions{
		UserIDs:  set.UserIDs,
		RoleIDs:  set.RoleIDs,
		Everyone: set.Everyone,
	}
}

func (s *messageServer) guildRoleSet(ctx context.Context, guildID, actorUserID int64) (map[int64]struct{}, error) {
	req := new(guildv1.ListGuildRolesRequest)
	req.SetGuildId(guildID)
	req.SetActorUserId(actorUserID)
	resp, err := s.svcCtx.GuildClient.ListGuildRoles(ctx, req)
	if err != nil {
		return nil, err
	}
	roles := make(map[int64]struct{}, len(resp.GetRoles()))
	for _, role := range resp.GetRoles() {
		if role.GetGuildId() == guildID && role.GetId() > 0 {
			roles[role.GetId()] = struct{}{}
		}
	}
	return roles, nil
}

func (s *messageServer) filterExistingUsers(ctx context.Context, userIDs []int64) ([]int64, error) {
	found := make(map[int64]struct{}, len(userIDs))
	for start := 0; start < len(userIDs); start += mentionUserBatchSize {
		end := min(start+mentionUserBatchSize, len(userIDs))
		req := new(userv1.BatchGetUserProfilesRequest)
		req.SetUserIds(userIDs[start:end])
		resp, err := s.svcCtx.UserClient.BatchGetUserProfiles(ctx, req)
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, status.Error(codes.Internal, "user service returned an invalid response")
		}
		for _, profile := range resp.GetProfiles() {
			if profile == nil || profile.GetUserId() <= 0 {
				continue
			}
			found[profile.GetUserId()] = struct{}{}
		}
	}
	values := make([]int64, 0, len(userIDs))
	for _, userID := range userIDs {
		if _, ok := found[userID]; ok {
			values = append(values, userID)
		}
	}
	return values, nil
}
