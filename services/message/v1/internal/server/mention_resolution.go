package server

import (
	"context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/services/message/v1/internal/mention"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

// resolveMentions parses content and reduces the mention set to entities the
// service is willing to persist. DM channels keep only user mentions; Guild
// channels require MENTION_EVERYONE for @everyone and drop roles and users
// that no longer exist, mirroring Discord's "only valid mentions" behavior.
func (s *messageServer) resolveMentions(ctx context.Context, content string, audience messageAudience, actorUserID int64) (model.MessageMentions, error) {
	parsed := mention.Parse(content)
	if audience.guildID == 0 {
		parsed.RoleIDs = nil
		parsed.Everyone = false
		return toMessageMentions(parsed), nil
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
	if len(parsed.UserIDs) > 0 {
		users, err := s.filterExistingUsers(ctx, parsed.UserIDs)
		if err != nil {
			return model.MessageMentions{}, err
		}
		parsed.UserIDs = users
	}
	return toMessageMentions(parsed), nil
}

func toMessageMentions(set mention.Set) model.MessageMentions {
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
	req := new(userv1.BatchGetUserProfilesRequest)
	req.SetUserIds(userIDs)
	resp, err := s.svcCtx.UserClient.BatchGetUserProfiles(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "user service returned an invalid response")
	}
	found := make(map[int64]struct{}, len(userIDs))
	for _, profile := range resp.GetProfiles() {
		if profile == nil || profile.GetUserId() <= 0 {
			continue
		}
		found[profile.GetUserId()] = struct{}{}
	}
	values := make([]int64, 0, len(userIDs))
	for _, userID := range userIDs {
		if _, ok := found[userID]; ok {
			values = append(values, userID)
		}
	}
	return values, nil
}
