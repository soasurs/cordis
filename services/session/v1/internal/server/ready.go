package server

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
)

type readyPayload struct {
	UserID               string           `json:"user_id"`
	AuthSessionID        string           `json:"auth_session_id"`
	SessionID            string           `json:"session_id"`
	SessionNodeID        string           `json:"session_node_id"`
	AccessTokenExpiresAt int64            `json:"access_token_expires_at"`
	Guilds               []readyGuild     `json:"guilds"`
	DmChannels           []readyDmChannel `json:"dm_channels"`
	ReadStates           []readyReadState `json:"read_states"`
}

type readyGuild struct {
	ID                   string                     `json:"id"`
	OwnerID              string                     `json:"owner_id"`
	Name                 string                     `json:"name"`
	IconAssetID          string                     `json:"icon_asset_id"`
	Revision             int64                      `json:"revision"`
	AccessRevision       int64                      `json:"access_revision"`
	CreatedAt            int64                      `json:"created_at"`
	UpdatedAt            int64                      `json:"updated_at"`
	Roles                []readyRole                `json:"roles"`
	MemberRoleIDs        []string                   `json:"member_role_ids"`
	Channels             []readyChannel             `json:"channels"`
	PermissionOverwrites []readyPermissionOverwrite `json:"permission_overwrites"`
}

type readyRole struct {
	ID          string `json:"id"`
	GuildID     string `json:"guild_id"`
	Name        string `json:"name"`
	Permissions string `json:"permissions"`
	Position    int32  `json:"position"`
	IsDefault   bool   `json:"is_default"`
	Revision    int64  `json:"revision"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type readyChannel struct {
	ID        string `json:"id"`
	GuildID   string `json:"guild_id"`
	Name      string `json:"name"`
	Type      int32  `json:"type"`
	Position  int32  `json:"position"`
	Topic     string `json:"topic"`
	Revision  int64  `json:"revision"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
	ParentID  string `json:"parent_id,omitempty"`
}

type readyPermissionOverwrite struct {
	ChannelID  string `json:"channel_id"`
	GuildID    string `json:"guild_id"`
	TargetType int32  `json:"target_type"`
	TargetID   string `json:"target_id"`
	Allow      string `json:"allow"`
	Deny       string `json:"deny"`
	Revision   int64  `json:"revision"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

type readyDmChannel struct {
	ID          string           `json:"id"`
	RecipientID string           `json:"recipient_id"`
	Recipient   readyUserProfile `json:"recipient"`
	CreatedAt   int64            `json:"created_at"`
}

type readyUserProfile struct {
	UserID        string `json:"user_id"`
	Name          string `json:"name"`
	AvatarAssetID string `json:"avatar_asset_id"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
	Username      string `json:"username"`
}

type readyReadState struct {
	ChannelID         string `json:"channel_id"`
	LastMessageID     string `json:"last_message_id"`
	LastReadMessageID string `json:"last_read_message_id"`
	MentionCount      int32  `json:"mention_count"`
}

func marshalReady(
	session *logicalSession,
	accessTokenExpiresAt int64,
	guilds []*guildv1.ReadyGuild,
	messages *messagev1.GetUserReadyStateResponse,
	profiles map[int64]*userv1.UserProfile,
	nodeID string,
) ([]byte, error) {
	payload := readyPayload{
		UserID:               idString(session.userID),
		AuthSessionID:        idString(session.authSessionID),
		SessionID:            session.id,
		SessionNodeID:        nodeID,
		AccessTokenExpiresAt: accessTokenExpiresAt,
		Guilds:               readyGuildValues(guilds),
		DmChannels:           readyDmChannelValues(session.userID, messages.GetDmChannels(), profiles),
		ReadStates:           readyReadStateValues(messages.GetReadStates()),
	}
	return json.Marshal(payload)
}

func readyGuildValues(values []*guildv1.ReadyGuild) []readyGuild {
	result := make([]readyGuild, 0, len(values))
	for _, value := range values {
		guild := value.GetGuild()
		roles := make([]readyRole, 0, len(value.GetRoles()))
		for _, role := range value.GetRoles() {
			roles = append(roles, readyRole{
				ID: idString(role.GetId()), GuildID: idString(role.GetGuildId()), Name: role.GetName(),
				Permissions: strconv.FormatUint(role.GetPermissions(), 10), Position: role.GetPosition(),
				IsDefault: role.GetIsDefault(), Revision: role.GetRevision(),
				CreatedAt: role.GetCreatedAt(), UpdatedAt: role.GetUpdatedAt(),
			})
		}
		channels := make([]readyChannel, 0, len(value.GetChannels()))
		for _, channel := range value.GetChannels() {
			channels = append(channels, readyChannel{
				ID: idString(channel.GetId()), GuildID: idString(channel.GetGuildId()), Name: channel.GetName(),
				Type: int32(channel.GetType()), Position: channel.GetPosition(), Topic: channel.GetTopic(),
				Revision: channel.GetRevision(), CreatedAt: channel.GetCreatedAt(), UpdatedAt: channel.GetUpdatedAt(),
				ParentID: optionalIDString(channel.GetParentId()),
			})
		}
		overwrites := make([]readyPermissionOverwrite, 0, len(value.GetPermissionOverwrites()))
		for _, overwrite := range value.GetPermissionOverwrites() {
			overwrites = append(overwrites, readyPermissionOverwrite{
				ChannelID: idString(overwrite.GetChannelId()), GuildID: idString(overwrite.GetGuildId()),
				TargetType: int32(overwrite.GetTargetType()), TargetID: idString(overwrite.GetTargetId()),
				Allow: strconv.FormatUint(overwrite.GetAllow(), 10), Deny: strconv.FormatUint(overwrite.GetDeny(), 10),
				Revision: overwrite.GetRevision(), CreatedAt: overwrite.GetCreatedAt(), UpdatedAt: overwrite.GetUpdatedAt(),
			})
		}
		result = append(result, readyGuild{
			ID: idString(guild.GetId()), OwnerID: idString(guild.GetOwnerId()), Name: guild.GetName(),
			IconAssetID: strconv.FormatInt(guild.GetIconAssetId(), 10),
			Revision:    guild.GetRevision(), AccessRevision: value.GetAccessRevision(),
			CreatedAt: guild.GetCreatedAt(), UpdatedAt: guild.GetUpdatedAt(), Roles: roles,
			MemberRoleIDs: stringifyIDs(value.GetMemberRoleIds()), Channels: channels,
			PermissionOverwrites: overwrites,
		})
	}
	return result
}

func readyDmChannelValues(
	userID int64,
	values []*messagev1.DmChannel,
	profiles map[int64]*userv1.UserProfile,
) []readyDmChannel {
	result := make([]readyDmChannel, 0, len(values))
	for _, value := range values {
		recipientID := value.GetUserLo()
		if recipientID == userID {
			recipientID = value.GetUserHi()
		}
		result = append(result, readyDmChannel{
			ID: idString(value.GetId()), RecipientID: idString(recipientID),
			Recipient: readyUserProfileFromProto(profiles[recipientID]), CreatedAt: value.GetCreatedAt(),
		})
	}
	return result
}

func readyUserProfileFromProto(profile *userv1.UserProfile) readyUserProfile {
	return readyUserProfile{
		UserID: idString(profile.GetUserId()), Name: profile.GetName(),
		AvatarAssetID: idString(profile.GetAvatarAssetId()), CreatedAt: profile.GetCreatedAt(),
		UpdatedAt: profile.GetUpdatedAt(), Username: profile.GetUsername(),
	}
}

func (s *Server) getReadyUserProfiles(
	ctx context.Context,
	userID int64,
	channels []*messagev1.DmChannel,
) (map[int64]*userv1.UserProfile, error) {
	const batchSize = 100
	userIDs := make([]int64, 0, len(channels))
	expected := make(map[int64]struct{}, len(channels))
	for _, channel := range channels {
		if channel == nil {
			return nil, status.Error(codes.Internal, "message service returned an invalid dm channel")
		}
		recipientID := channel.GetUserLo()
		if recipientID == userID {
			recipientID = channel.GetUserHi()
		}
		if recipientID <= 0 {
			return nil, status.Error(codes.Internal, "message service returned an invalid dm recipient")
		}
		if _, ok := expected[recipientID]; ok {
			continue
		}
		expected[recipientID] = struct{}{}
		userIDs = append(userIDs, recipientID)
	}

	profiles := make(map[int64]*userv1.UserProfile, len(userIDs))
	for chunk := range slices.Chunk(userIDs, batchSize) {
		req := new(userv1.BatchGetUserProfilesRequest)
		req.SetUserIds(chunk)
		resp, err := s.svcCtx.UserClient.BatchGetUserProfiles(ctx, req)
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, status.Error(codes.Internal, "user service returned an invalid response")
		}
		for _, profile := range resp.GetProfiles() {
			if profile == nil {
				return nil, status.Error(codes.Internal, "user service returned an invalid profile")
			}
			profileUserID := profile.GetUserId()
			if _, ok := expected[profileUserID]; !ok || profiles[profileUserID] != nil {
				return nil, status.Error(codes.Internal, "user service returned unexpected profiles")
			}
			profiles[profileUserID] = profile
		}
	}
	for _, profileUserID := range userIDs {
		if profiles[profileUserID] == nil {
			return nil, status.Error(codes.Internal, "user service did not return all profiles")
		}
	}
	return profiles, nil
}

func readyReadStateValues(values []*messagev1.ChannelReadState) []readyReadState {
	result := make([]readyReadState, 0, len(values))
	for _, value := range values {
		result = append(result, readyReadState{
			ChannelID: idString(value.GetChannelId()), LastMessageID: idString(value.GetLastMessageId()),
			LastReadMessageID: idString(value.GetLastReadMessageId()), MentionCount: value.GetMentionCount(),
		})
	}
	return result
}

func idString(id int64) string {
	return strconv.FormatInt(id, 10)
}

func optionalIDString(id int64) string {
	if id == 0 {
		return ""
	}
	return idString(id)
}

func readyGuildTextChannelIDs(guilds []*guildv1.ReadyGuild) []int64 {
	var ids []int64
	for _, guild := range guilds {
		for _, channel := range guild.GetChannels() {
			if channel.GetType() == guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT {
				ids = append(ids, channel.GetId())
			}
		}
	}
	return ids
}
