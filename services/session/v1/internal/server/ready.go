package server

import (
	"encoding/json"
	"strconv"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
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
	ID          string `json:"id"`
	RecipientID string `json:"recipient_id"`
	CreatedAt   int64  `json:"created_at"`
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
	nodeID string,
) ([]byte, error) {
	payload := readyPayload{
		UserID:               idString(session.userID),
		AuthSessionID:        idString(session.authSessionID),
		SessionID:            session.id,
		SessionNodeID:        nodeID,
		AccessTokenExpiresAt: accessTokenExpiresAt,
		Guilds:               readyGuildValues(guilds),
		DmChannels:           readyDmChannelValues(session.userID, messages.GetDmChannels()),
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

func readyDmChannelValues(userID int64, values []*messagev1.DmChannel) []readyDmChannel {
	result := make([]readyDmChannel, 0, len(values))
	for _, value := range values {
		recipientID := value.GetUserLo()
		if recipientID == userID {
			recipientID = value.GetUserHi()
		}
		result = append(result, readyDmChannel{
			ID: idString(value.GetId()), RecipientID: idString(recipientID), CreatedAt: value.GetCreatedAt(),
		})
	}
	return result
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
