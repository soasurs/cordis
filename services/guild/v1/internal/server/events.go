package server

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

const (
	EventTypeGuildCreated                 = "guild.created"
	EventTypeGuildUpdated                 = "guild.updated"
	EventTypeGuildDeleted                 = "guild.deleted"
	EventTypeGuildMemberJoined            = "guild.member.joined"
	EventTypeGuildMemberUpdated           = "guild.member.updated"
	EventTypeGuildMemberRemoved           = "guild.member.removed"
	EventTypeGuildRoleCreated             = "guild.role.created"
	EventTypeGuildRoleUpdated             = "guild.role.updated"
	EventTypeGuildRoleDeleted             = "guild.role.deleted"
	EventTypeGuildMemberRolesUpdated      = "guild.member.roles.updated"
	EventTypeGuildChannelCreated          = "guild.channel.created"
	EventTypeGuildChannelUpdated          = "guild.channel.updated"
	EventTypeGuildChannelDeleted          = "guild.channel.deleted"
	EventTypeGuildChannelOverwriteUpdated = "guild.channel.overwrite.updated"
	EventTypeGuildChannelOverwriteDeleted = "guild.channel.overwrite.deleted"
)

type eventEnvelope[T any] struct {
	Type string `json:"t"`
	Data T      `json:"d"`
}

type guildEvent struct {
	Key     []byte
	Payload []byte
}

type guildPayload struct {
	ID        string `json:"id"`
	OwnerID   string `json:"owner_id"`
	Name      string `json:"name"`
	IconURI   string `json:"icon_uri"`
	Revision  int64  `json:"revision"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type guildDeletedPayload struct {
	ID        string `json:"id"`
	Revision  int64  `json:"revision"`
	DeletedAt int64  `json:"deleted_at"`
}

type guildMemberPayload struct {
	GuildID   string `json:"guild_id"`
	UserID    string `json:"user_id"`
	Nickname  string `json:"nickname"`
	Revision  int64  `json:"revision"`
	JoinedAt  int64  `json:"joined_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type guildMemberRemovedPayload struct {
	GuildID   string `json:"guild_id"`
	UserID    string `json:"user_id"`
	Revision  int64  `json:"revision"`
	RemovedAt int64  `json:"removed_at"`
}

type guildRolePayload struct {
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

type guildRoleDeletedPayload struct {
	ID        string `json:"id"`
	GuildID   string `json:"guild_id"`
	Revision  int64  `json:"revision"`
	DeletedAt int64  `json:"deleted_at"`
}

type guildMemberRolesUpdatedPayload struct {
	GuildID   string   `json:"guild_id"`
	UserID    string   `json:"user_id"`
	RoleIDs   []string `json:"role_ids"`
	UpdatedAt int64    `json:"updated_at"`
}

type guildChannelPayload struct {
	ID        string `json:"id"`
	GuildID   string `json:"guild_id"`
	Name      string `json:"name"`
	Type      int32  `json:"type"`
	Position  int32  `json:"position"`
	Topic     string `json:"topic"`
	Revision  int64  `json:"revision"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type guildChannelDeletedPayload struct {
	ID        string `json:"id"`
	GuildID   string `json:"guild_id"`
	Revision  int64  `json:"revision"`
	DeletedAt int64  `json:"deleted_at"`
}

type guildChannelOverwritePayload struct {
	ChannelID  string `json:"channel_id"`
	GuildID    string `json:"guild_id"`
	TargetType int32  `json:"target_type"`
	TargetID   string `json:"target_id"`
	Allow      string `json:"allow"`
	Deny       string `json:"deny"`
	Revision   int64  `json:"revision"`
	UpdatedAt  int64  `json:"updated_at"`
}

type guildChannelOverwriteDeletedPayload struct {
	ChannelID  string `json:"channel_id"`
	GuildID    string `json:"guild_id"`
	TargetType int32  `json:"target_type"`
	TargetID   string `json:"target_id"`
}

func newGuildCreatedEvent(guild *model.Guild) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildCreated, guild.ID, guildPayloadFromModel(guild))
}

func newGuildMemberJoinedEvent(member *model.GuildMember) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildMemberJoined, member.GuildID, guildMemberPayloadFromModel(member))
}

func newGuildMemberUpdatedEvent(member *model.GuildMember) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildMemberUpdated, member.GuildID, guildMemberPayloadFromModel(member))
}

func newGuildMemberRemovedEvent(member *model.GuildMember) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildMemberRemoved, member.GuildID, guildMemberRemovedPayload{
		GuildID:   strconv.FormatInt(member.GuildID, 10),
		UserID:    strconv.FormatInt(member.UserID, 10),
		Revision:  member.Revision,
		RemovedAt: member.DeletedAt,
	})
}

func guildMemberPayloadFromModel(member *model.GuildMember) guildMemberPayload {
	return guildMemberPayload{
		GuildID:   strconv.FormatInt(member.GuildID, 10),
		UserID:    strconv.FormatInt(member.UserID, 10),
		Nickname:  member.Nickname,
		Revision:  member.Revision,
		JoinedAt:  member.JoinedAt,
		UpdatedAt: member.UpdatedAt,
	}
}

func newGuildRoleCreatedEvent(role *model.Role) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildRoleCreated, role.GuildID, guildRolePayloadFromModel(role))
}

func newGuildRoleUpdatedEvent(role *model.Role) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildRoleUpdated, role.GuildID, guildRolePayloadFromModel(role))
}

func newGuildRoleDeletedEvent(role *model.Role) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildRoleDeleted, role.GuildID, guildRoleDeletedPayload{
		ID:        strconv.FormatInt(role.ID, 10),
		GuildID:   strconv.FormatInt(role.GuildID, 10),
		Revision:  role.Revision,
		DeletedAt: role.DeletedAt,
	})
}

func newGuildMemberRolesUpdatedEvent(guildID, userID int64, roles []*model.Role, updatedAt int64) (guildEvent, error) {
	roleIDs := make([]string, 0, len(roles))
	for _, role := range roles {
		if !role.IsDefault {
			roleIDs = append(roleIDs, strconv.FormatInt(role.ID, 10))
		}
	}
	return newGuildEvent(EventTypeGuildMemberRolesUpdated, guildID, guildMemberRolesUpdatedPayload{
		GuildID:   strconv.FormatInt(guildID, 10),
		UserID:    strconv.FormatInt(userID, 10),
		RoleIDs:   roleIDs,
		UpdatedAt: updatedAt,
	})
}

func guildRolePayloadFromModel(role *model.Role) guildRolePayload {
	return guildRolePayload{
		ID:          strconv.FormatInt(role.ID, 10),
		GuildID:     strconv.FormatInt(role.GuildID, 10),
		Name:        role.Name,
		Permissions: strconv.FormatUint(role.Permissions, 10),
		Position:    role.Position,
		IsDefault:   role.IsDefault,
		Revision:    role.Revision,
		CreatedAt:   role.CreatedAt,
		UpdatedAt:   role.UpdatedAt,
	}
}

func newGuildChannelCreatedEvent(channel *model.Channel) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildChannelCreated, channel.GuildID, guildChannelPayloadFromModel(channel))
}

func newGuildChannelUpdatedEvent(channel *model.Channel) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildChannelUpdated, channel.GuildID, guildChannelPayloadFromModel(channel))
}

func newGuildChannelDeletedEvent(channel *model.Channel) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildChannelDeleted, channel.GuildID, guildChannelDeletedPayload{
		ID: strconv.FormatInt(channel.ID, 10), GuildID: strconv.FormatInt(channel.GuildID, 10),
		Revision: channel.Revision, DeletedAt: channel.DeletedAt,
	})
}

func newGuildChannelOverwriteUpdatedEvent(overwrite *model.ChannelPermissionOverwrite) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildChannelOverwriteUpdated, overwrite.GuildID, guildChannelOverwritePayload{
		ChannelID:  strconv.FormatInt(overwrite.ChannelID, 10),
		GuildID:    strconv.FormatInt(overwrite.GuildID, 10),
		TargetType: overwrite.TargetType,
		TargetID:   strconv.FormatInt(overwrite.TargetID, 10),
		Allow:      strconv.FormatUint(overwrite.Allow, 10),
		Deny:       strconv.FormatUint(overwrite.Deny, 10),
		Revision:   overwrite.Revision,
		UpdatedAt:  overwrite.UpdatedAt,
	})
}

func newGuildChannelOverwriteDeletedEvent(guildID, channelID int64, targetType int32, targetID int64) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildChannelOverwriteDeleted, guildID, guildChannelOverwriteDeletedPayload{
		ChannelID: strconv.FormatInt(channelID, 10), GuildID: strconv.FormatInt(guildID, 10),
		TargetType: targetType, TargetID: strconv.FormatInt(targetID, 10),
	})
}

func guildChannelPayloadFromModel(channel *model.Channel) guildChannelPayload {
	return guildChannelPayload{
		ID: strconv.FormatInt(channel.ID, 10), GuildID: strconv.FormatInt(channel.GuildID, 10),
		Name: channel.Name, Type: channel.Type, Position: channel.Position, Topic: channel.Topic,
		Revision: channel.Revision, CreatedAt: channel.CreatedAt, UpdatedAt: channel.UpdatedAt,
	}
}

func newGuildUpdatedEvent(guild *model.Guild) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildUpdated, guild.ID, guildPayloadFromModel(guild))
}

func newGuildDeletedEvent(guild *model.Guild) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildDeleted, guild.ID, guildDeletedPayload{
		ID:        strconv.FormatInt(guild.ID, 10),
		Revision:  guild.Revision,
		DeletedAt: guild.DeletedAt,
	})
}

func guildPayloadFromModel(guild *model.Guild) guildPayload {
	return guildPayload{
		ID:        strconv.FormatInt(guild.ID, 10),
		OwnerID:   strconv.FormatInt(guild.OwnerID, 10),
		Name:      guild.Name,
		IconURI:   guild.IconURI,
		Revision:  guild.Revision,
		CreatedAt: guild.CreatedAt,
		UpdatedAt: guild.UpdatedAt,
	}
}

func newGuildEvent[T any](eventType string, guildID int64, data T) (guildEvent, error) {
	payload, err := json.Marshal(eventEnvelope[T]{Type: eventType, Data: data})
	if err != nil {
		return guildEvent{}, fmt.Errorf("marshal %s event: %w", eventType, err)
	}
	return guildEvent{
		Key:     strconv.AppendInt(nil, guildID, 10),
		Payload: payload,
	}, nil
}
