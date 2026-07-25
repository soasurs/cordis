package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

const (
	EventTypeGuildCreated                 = realtime.EventGuildCreated
	EventTypeGuildUpdated                 = realtime.EventGuildUpdated
	EventTypeGuildDeleted                 = realtime.EventGuildDeleted
	EventTypeGuildMemberJoined            = realtime.EventGuildMemberJoined
	EventTypeGuildMemberUpdated           = realtime.EventGuildMemberUpdated
	EventTypeGuildMemberRemoved           = realtime.EventGuildMemberRemoved
	EventTypeGuildMemberBanned            = realtime.EventGuildMemberBanned
	EventTypeGuildMemberUnbanned          = realtime.EventGuildMemberUnbanned
	EventTypeGuildRoleCreated             = realtime.EventGuildRoleCreated
	EventTypeGuildRoleUpdated             = realtime.EventGuildRoleUpdated
	EventTypeGuildRoleDeleted             = realtime.EventGuildRoleDeleted
	EventTypeGuildMemberRolesUpdated      = realtime.EventGuildMemberRolesUpdated
	EventTypeGuildChannelCreated          = realtime.EventGuildChannelCreated
	EventTypeGuildChannelUpdated          = realtime.EventGuildChannelUpdated
	EventTypeGuildChannelDeleted          = realtime.EventGuildChannelDeleted
	EventTypeGuildChannelOverwriteUpdated = realtime.EventGuildChannelOverwriteUpdated
	EventTypeGuildChannelOverwriteDeleted = realtime.EventGuildChannelOverwriteDeleted
)

type eventEnvelope[T any] struct {
	Type           string `json:"t"`
	Data           T      `json:"d"`
	IdempotencyKey string `json:"idempotency_key"`
}

type guildEvent struct {
	Type    string
	GuildID int64
	Key     []byte
	Payload []byte
}

type guildPayload struct {
	ID          string `json:"id"`
	OwnerID     string `json:"owner_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IconAssetID string `json:"icon_asset_id"`
	Revision    int64  `json:"revision"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type guildDeletedPayload struct {
	ID        string `json:"id"`
	Revision  int64  `json:"revision"`
	DeletedAt int64  `json:"deleted_at"`
}

type guildMemberPayload struct {
	GuildID   string             `json:"guild_id"`
	UserID    string             `json:"user_id"`
	Profile   userProfilePayload `json:"profile"`
	Nickname  string             `json:"nickname"`
	Revision  int64              `json:"revision"`
	JoinedAt  int64              `json:"joined_at"`
	UpdatedAt int64              `json:"updated_at"`
}

type guildMemberRemovedPayload struct {
	GuildID   string `json:"guild_id"`
	UserID    string `json:"user_id"`
	Revision  int64  `json:"revision"`
	RemovedAt int64  `json:"removed_at"`
}

type guildMemberBannedPayload struct {
	GuildID      string             `json:"guild_id"`
	UserID       string             `json:"user_id"`
	ActorUserID  string             `json:"actor_user_id"`
	Profile      userProfilePayload `json:"profile"`
	ActorProfile userProfilePayload `json:"actor_profile"`
	Reason       string             `json:"reason"`
	BannedAt     int64              `json:"banned_at"`
}

type guildMemberUnbannedPayload struct {
	GuildID    string             `json:"guild_id"`
	UserID     string             `json:"user_id"`
	Profile    userProfilePayload `json:"profile"`
	UnbannedAt int64              `json:"unbanned_at"`
}

type userProfilePayload struct {
	UserID        string `json:"user_id"`
	Name          string `json:"name"`
	AvatarAssetID string `json:"avatar_asset_id"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
	Username      string `json:"username"`
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
	ParentID  string `json:"parent_id"`
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

func newGuildCreatedEvent(guild *model.Guild, idempotencyKey int64) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildCreated, guild.ID, guildPayloadFromModel(guild), idempotencyKey)
}

func newGuildMemberJoinedEvent(member *model.GuildMember, profile *userv1.UserProfile, idempotencyKey int64) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildMemberJoined, member.GuildID, guildMemberPayloadFromModel(member, profile), idempotencyKey)
}

func newGuildMemberUpdatedEvent(member *model.GuildMember, profile *userv1.UserProfile, idempotencyKey int64) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildMemberUpdated, member.GuildID, guildMemberPayloadFromModel(member, profile), idempotencyKey)
}

func newGuildMemberRemovedEvent(member *model.GuildMember, idempotencyKey int64) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildMemberRemoved, member.GuildID, guildMemberRemovedPayload{
		GuildID:   strconv.FormatInt(member.GuildID, 10),
		UserID:    strconv.FormatInt(member.UserID, 10),
		Revision:  member.Revision,
		RemovedAt: member.DeletedAt,
	}, idempotencyKey)
}

func newGuildMemberBannedEvent(
	ban *model.GuildBan,
	profile, actorProfile *userv1.UserProfile,
	idempotencyKey int64,
) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildMemberBanned, ban.GuildID, guildMemberBannedPayload{
		GuildID: strconv.FormatInt(ban.GuildID, 10), UserID: strconv.FormatInt(ban.UserID, 10),
		ActorUserID: strconv.FormatInt(ban.ActorUserID, 10), Profile: userProfilePayloadFromProto(profile),
		ActorProfile: userProfilePayloadFromProto(actorProfile), Reason: ban.Reason, BannedAt: ban.CreatedAt,
	}, idempotencyKey)
}

func newGuildMemberUnbannedEvent(
	guildID, userID, unbannedAt int64,
	profile *userv1.UserProfile,
	idempotencyKey int64,
) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildMemberUnbanned, guildID, guildMemberUnbannedPayload{
		GuildID: strconv.FormatInt(guildID, 10), UserID: strconv.FormatInt(userID, 10),
		Profile: userProfilePayloadFromProto(profile), UnbannedAt: unbannedAt,
	}, idempotencyKey)
}

func guildMemberPayloadFromModel(member *model.GuildMember, profile *userv1.UserProfile) guildMemberPayload {
	return guildMemberPayload{
		GuildID:   strconv.FormatInt(member.GuildID, 10),
		UserID:    strconv.FormatInt(member.UserID, 10),
		Profile:   userProfilePayloadFromProto(profile),
		Nickname:  member.Nickname,
		Revision:  member.Revision,
		JoinedAt:  member.JoinedAt,
		UpdatedAt: member.UpdatedAt,
	}
}

func userProfilePayloadFromProto(profile *userv1.UserProfile) userProfilePayload {
	if profile == nil {
		return userProfilePayload{}
	}
	return userProfilePayload{
		UserID:        strconv.FormatInt(profile.GetUserId(), 10),
		Name:          profile.GetName(),
		AvatarAssetID: strconv.FormatInt(profile.GetAvatarAssetId(), 10),
		CreatedAt:     profile.GetCreatedAt(),
		UpdatedAt:     profile.GetUpdatedAt(),
		Username:      profile.GetUsername(),
	}
}

func (s *guildServer) getEventUserProfiles(
	ctx context.Context,
	userIDs ...int64,
) (map[int64]*userv1.UserProfile, error) {
	req := new(userv1.BatchGetUserProfilesRequest)
	req.SetUserIds(userIDs)
	resp, err := s.svcCtx.UserClient.BatchGetUserProfiles(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "user service returned an invalid response")
	}
	expected := make(map[int64]struct{}, len(userIDs))
	for _, userID := range userIDs {
		expected[userID] = struct{}{}
	}
	profiles := make(map[int64]*userv1.UserProfile, len(expected))
	for _, profile := range resp.GetProfiles() {
		if profile == nil {
			return nil, status.Error(codes.Internal, "user service returned an invalid profile")
		}
		userID := profile.GetUserId()
		if _, ok := expected[userID]; !ok || profiles[userID] != nil {
			return nil, status.Error(codes.Internal, "user service returned unexpected profiles")
		}
		profiles[userID] = profile
	}
	for userID := range expected {
		if profiles[userID] == nil {
			return nil, status.Error(codes.Internal, "user service did not return all profiles")
		}
	}
	return profiles, nil
}

func newGuildRoleCreatedEvent(role *model.Role, idempotencyKey int64) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildRoleCreated, role.GuildID, guildRolePayloadFromModel(role), idempotencyKey)
}

func newGuildRoleUpdatedEvent(role *model.Role, idempotencyKey int64) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildRoleUpdated, role.GuildID, guildRolePayloadFromModel(role), idempotencyKey)
}

func newGuildRoleDeletedEvent(role *model.Role, idempotencyKey int64) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildRoleDeleted, role.GuildID, guildRoleDeletedPayload{
		ID:        strconv.FormatInt(role.ID, 10),
		GuildID:   strconv.FormatInt(role.GuildID, 10),
		Revision:  role.Revision,
		DeletedAt: role.DeletedAt,
	}, idempotencyKey)
}

func newGuildMemberRolesUpdatedEvent(guildID, userID int64, roles []*model.Role, updatedAt int64, idempotencyKey int64) (guildEvent, error) {
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
	}, idempotencyKey)
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

func newGuildChannelCreatedEvent(channel *model.Channel, idempotencyKey int64) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildChannelCreated, channel.GuildID, guildChannelPayloadFromModel(channel), idempotencyKey)
}

func newGuildChannelUpdatedEvent(channel *model.Channel, idempotencyKey int64) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildChannelUpdated, channel.GuildID, guildChannelPayloadFromModel(channel), idempotencyKey)
}

func newGuildChannelDeletedEvent(channel *model.Channel, idempotencyKey int64) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildChannelDeleted, channel.GuildID, guildChannelDeletedPayload{
		ID: strconv.FormatInt(channel.ID, 10), GuildID: strconv.FormatInt(channel.GuildID, 10),
		Revision: channel.Revision, DeletedAt: channel.DeletedAt,
	}, idempotencyKey)
}

func newGuildChannelOverwriteUpdatedEvent(overwrite *model.ChannelPermissionOverwrite, idempotencyKey int64) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildChannelOverwriteUpdated, overwrite.GuildID, guildChannelOverwritePayload{
		ChannelID:  strconv.FormatInt(overwrite.ChannelID, 10),
		GuildID:    strconv.FormatInt(overwrite.GuildID, 10),
		TargetType: overwrite.TargetType,
		TargetID:   strconv.FormatInt(overwrite.TargetID, 10),
		Allow:      strconv.FormatUint(overwrite.Allow, 10),
		Deny:       strconv.FormatUint(overwrite.Deny, 10),
		Revision:   overwrite.Revision,
		UpdatedAt:  overwrite.UpdatedAt,
	}, idempotencyKey)
}

func newGuildChannelOverwriteDeletedEvent(guildID, channelID int64, targetType int32, targetID int64, idempotencyKey int64) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildChannelOverwriteDeleted, guildID, guildChannelOverwriteDeletedPayload{
		ChannelID: strconv.FormatInt(channelID, 10), GuildID: strconv.FormatInt(guildID, 10),
		TargetType: targetType, TargetID: strconv.FormatInt(targetID, 10),
	}, idempotencyKey)
}

func guildChannelPayloadFromModel(channel *model.Channel) guildChannelPayload {
	return guildChannelPayload{
		ID: strconv.FormatInt(channel.ID, 10), GuildID: strconv.FormatInt(channel.GuildID, 10),
		Name: channel.Name, Type: channel.Type, Position: channel.Position, Topic: channel.Topic,
		Revision: channel.Revision, CreatedAt: channel.CreatedAt, UpdatedAt: channel.UpdatedAt,
		ParentID: strconv.FormatInt(channel.ParentID, 10),
	}
}

func newGuildUpdatedEvent(guild *model.Guild, idempotencyKey int64) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildUpdated, guild.ID, guildPayloadFromModel(guild), idempotencyKey)
}

func newGuildDeletedEvent(guild *model.Guild, idempotencyKey int64) (guildEvent, error) {
	return newGuildEvent(EventTypeGuildDeleted, guild.ID, guildDeletedPayload{
		ID:        strconv.FormatInt(guild.ID, 10),
		Revision:  guild.Revision,
		DeletedAt: guild.DeletedAt,
	}, idempotencyKey)
}

func guildPayloadFromModel(guild *model.Guild) guildPayload {
	return guildPayload{
		ID:          strconv.FormatInt(guild.ID, 10),
		OwnerID:     strconv.FormatInt(guild.OwnerID, 10),
		Name:        guild.Name,
		Description: guild.Description,
		IconAssetID: strconv.FormatInt(guild.IconAssetID, 10),
		Revision:    guild.Revision,
		CreatedAt:   guild.CreatedAt,
		UpdatedAt:   guild.UpdatedAt,
	}
}

func newGuildEvent[T any](eventType string, guildID int64, data T, idempotencyKey int64) (guildEvent, error) {
	payload, err := json.Marshal(eventEnvelope[T]{Type: eventType, Data: data, IdempotencyKey: strconv.FormatInt(idempotencyKey, 10)})
	if err != nil {
		return guildEvent{}, fmt.Errorf("marshal %s event: %w", eventType, err)
	}
	return guildEvent{
		Type:    eventType,
		GuildID: guildID,
		Key:     strconv.AppendInt(nil, guildID, 10),
		Payload: payload,
	}, nil
}
