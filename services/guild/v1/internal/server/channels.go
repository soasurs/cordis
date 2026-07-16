package server

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

const (
	maxChannelNameRunes  = 100
	maxChannelTopicRunes = 1024
)

func (s *guildServer) CreateGuildChannel(ctx context.Context, req *guildv1.CreateGuildChannelRequest) (*guildv1.CreateGuildChannelResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	name, err := normalizeChannelName(req.GetName())
	if err != nil {
		return nil, err
	}
	channelType, err := normalizeChannelType(req.GetType())
	if err != nil {
		return nil, err
	}
	if err := validateChannelTopic(req.GetTopic()); err != nil {
		return nil, err
	}

	var channel *model.Channel
	createdAt := time.Now().UnixMilli()
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		authority, err := loadMemberAuthority(ctx, txStore, req.GetGuildId(), req.GetActorUserId())
		if err != nil {
			return err
		}
		if !authority.has(PermissionManageChannels) {
			return permissionDenied()
		}
		channels, err := txStore.ListGuildChannels(ctx, req.GetGuildId())
		if err != nil {
			return err
		}
		channel, err = txStore.CreateGuildChannel(
			ctx, s.svcCtx.Snowflake.Generate().Int64(), req.GetGuildId(), name,
			int32(channelType), int32(len(channels)), req.GetTopic(), createdAt,
		)
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	event, eventErr := newGuildChannelCreatedEvent(channel)
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.CreateGuildChannelResponse)
	resp.SetChannel(guildChannelToProto(channel))
	return resp, nil
}

func (s *guildServer) GetGuildChannel(ctx context.Context, req *guildv1.GetGuildChannelRequest) (*guildv1.GetGuildChannelResponse, error) {
	if err := validateChannelActorRequest(req.GetChannelId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	channel, permissions, err := s.loadAuthorizedChannel(ctx, req.GetChannelId(), req.GetActorUserId())
	if err != nil {
		return nil, err
	}
	if permissions&PermissionViewChannel == 0 {
		return nil, notFound()
	}
	resp := new(guildv1.GetGuildChannelResponse)
	resp.SetChannel(guildChannelToProto(channel))
	return resp, nil
}

func (s *guildServer) ListGuildChannels(ctx context.Context, req *guildv1.ListGuildChannelsRequest) (*guildv1.ListGuildChannelsResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	authority, err := loadMemberAuthority(ctx, s.svcCtx.Store, req.GetGuildId(), req.GetActorUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	roles, err := s.svcCtx.Store.ListGuildMemberRoles(ctx, req.GetGuildId(), req.GetActorUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	channels, err := s.svcCtx.Store.ListGuildChannels(ctx, req.GetGuildId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	visible := make([]*model.Channel, 0, len(channels))
	for _, channel := range channels {
		overwrites, err := s.svcCtx.Store.ListGuildChannelPermissionOverwrites(ctx, channel.ID)
		if err != nil {
			return nil, mapStoreError(err)
		}
		if channelPermissions(authority, roles, overwrites, req.GetActorUserId())&PermissionViewChannel != 0 {
			visible = append(visible, channel)
		}
	}
	resp := new(guildv1.ListGuildChannelsResponse)
	resp.SetChannels(guildChannelsToProto(visible))
	return resp, nil
}

func (s *guildServer) UpdateGuildChannel(ctx context.Context, req *guildv1.UpdateGuildChannelRequest) (*guildv1.UpdateGuildChannelResponse, error) {
	if err := validateChannelActorRequest(req.GetChannelId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	params := store.UpdateGuildChannelParams{ChannelID: req.GetChannelId(), UpdatedAt: time.Now().UnixMilli()}
	if req.HasName() {
		name, err := normalizeChannelName(req.GetName())
		if err != nil {
			return nil, err
		}
		params.Name = &name
	}
	if req.HasTopic() {
		if err := validateChannelTopic(req.GetTopic()); err != nil {
			return nil, err
		}
		topic := req.GetTopic()
		params.Topic = &topic
	}
	if params.Name == nil && params.Topic == nil {
		return nil, invalidRequest("at least one channel field is required")
	}

	var updated *model.Channel
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		channel, err := txStore.GetGuildChannel(ctx, req.GetChannelId())
		if err != nil {
			return err
		}
		authority, err := loadMemberAuthority(ctx, txStore, channel.GuildID, req.GetActorUserId())
		if err != nil {
			return err
		}
		if !authority.has(PermissionManageChannels) {
			return permissionDenied()
		}
		updated, err = txStore.UpdateGuildChannel(ctx, params)
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	event, eventErr := newGuildChannelUpdatedEvent(updated)
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.UpdateGuildChannelResponse)
	resp.SetChannel(guildChannelToProto(updated))
	return resp, nil
}

func (s *guildServer) DeleteGuildChannel(ctx context.Context, req *guildv1.DeleteGuildChannelRequest) (*guildv1.DeleteGuildChannelResponse, error) {
	if err := validateChannelActorRequest(req.GetChannelId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	var deleted *model.Channel
	deletedAt := time.Now().UnixMilli()
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		channel, err := txStore.GetGuildChannel(ctx, req.GetChannelId())
		if err != nil {
			return err
		}
		authority, err := loadMemberAuthority(ctx, txStore, channel.GuildID, req.GetActorUserId())
		if err != nil {
			return err
		}
		if !authority.has(PermissionManageChannels) {
			return permissionDenied()
		}
		if err := txStore.DeleteGuildChannelPermissionOverwrites(ctx, channel.ID); err != nil {
			return err
		}
		deleted, err = txStore.DeleteGuildChannel(ctx, channel.ID, deletedAt)
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	event, eventErr := newGuildChannelDeletedEvent(deleted)
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.DeleteGuildChannelResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *guildServer) ReorderGuildChannels(ctx context.Context, req *guildv1.ReorderGuildChannelsRequest) (*guildv1.ReorderGuildChannelsResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if len(req.GetPositions()) == 0 {
		return nil, invalidRequest("channel positions are required")
	}
	positions := make(map[int64]int32, len(req.GetPositions()))
	for _, item := range req.GetPositions() {
		if item.GetChannelId() <= 0 || item.GetPosition() < 0 {
			return nil, invalidRequest("channel id and position are invalid")
		}
		if _, exists := positions[item.GetChannelId()]; exists {
			return nil, invalidRequest("channel id must be unique")
		}
		positions[item.GetChannelId()] = item.GetPosition()
	}

	var channels []*model.Channel
	var updated []*model.Channel
	updatedAt := time.Now().UnixMilli()
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		authority, err := loadMemberAuthority(ctx, txStore, req.GetGuildId(), req.GetActorUserId())
		if err != nil {
			return err
		}
		if !authority.has(PermissionManageChannels) {
			return permissionDenied()
		}
		current, err := txStore.ListGuildChannels(ctx, req.GetGuildId())
		if err != nil {
			return err
		}
		if err := validateChannelPositions(current, positions); err != nil {
			return err
		}
		for channelID, position := range positions {
			channel, err := txStore.GetGuildChannel(ctx, channelID)
			if err != nil {
				return err
			}
			if channel.GuildID != req.GetGuildId() {
				return notFound()
			}
			channel, err = txStore.UpdateGuildChannelPosition(ctx, channelID, position, updatedAt)
			if err != nil {
				return err
			}
			updated = append(updated, channel)
		}
		channels, err = txStore.ListGuildChannels(ctx, req.GetGuildId())
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	for _, channel := range updated {
		event, eventErr := newGuildChannelUpdatedEvent(channel)
		s.publishEvent(ctx, event, eventErr)
	}
	resp := new(guildv1.ReorderGuildChannelsResponse)
	resp.SetChannels(guildChannelsToProto(channels))
	return resp, nil
}

func (s *guildServer) UpsertGuildChannelPermissionOverwrite(
	ctx context.Context,
	req *guildv1.UpsertGuildChannelPermissionOverwriteRequest,
) (*guildv1.UpsertGuildChannelPermissionOverwriteResponse, error) {
	if err := validateOverwriteRequest(req.GetChannelId(), req.GetActorUserId(), req.GetTargetType(), req.GetTargetId(), req.GetAllow(), req.GetDeny()); err != nil {
		return nil, err
	}
	var overwrite *model.ChannelPermissionOverwrite
	changedAt := time.Now().UnixMilli()
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		channel, err := txStore.GetGuildChannel(ctx, req.GetChannelId())
		if err != nil {
			return err
		}
		authority, err := loadMemberAuthority(ctx, txStore, channel.GuildID, req.GetActorUserId())
		if err != nil {
			return err
		}
		if !authority.has(PermissionManageChannels) || !authority.canGrantPermissions(req.GetAllow()) {
			return permissionDenied()
		}
		if err := validateOverwriteTarget(ctx, txStore, authority, channel.GuildID, req.GetTargetType(), req.GetTargetId()); err != nil {
			return err
		}
		overwrite, err = txStore.UpsertGuildChannelPermissionOverwrite(ctx, &model.ChannelPermissionOverwrite{
			ChannelID: channel.ID, GuildID: channel.GuildID, TargetType: int32(req.GetTargetType()),
			TargetID: req.GetTargetId(), Allow: req.GetAllow(), Deny: req.GetDeny(), CreatedAt: changedAt,
		})
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	event, eventErr := newGuildChannelOverwriteUpdatedEvent(overwrite)
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.UpsertGuildChannelPermissionOverwriteResponse)
	resp.SetOverwrite(guildChannelOverwriteToProto(overwrite))
	return resp, nil
}

func (s *guildServer) DeleteGuildChannelPermissionOverwrite(
	ctx context.Context,
	req *guildv1.DeleteGuildChannelPermissionOverwriteRequest,
) (*guildv1.DeleteGuildChannelPermissionOverwriteResponse, error) {
	if err := validateOverwriteRequest(req.GetChannelId(), req.GetActorUserId(), req.GetTargetType(), req.GetTargetId(), 0, 0); err != nil {
		return nil, err
	}
	var guildID int64
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		channel, err := txStore.GetGuildChannel(ctx, req.GetChannelId())
		if err != nil {
			return err
		}
		guildID = channel.GuildID
		authority, err := loadMemberAuthority(ctx, txStore, channel.GuildID, req.GetActorUserId())
		if err != nil {
			return err
		}
		if !authority.has(PermissionManageChannels) {
			return permissionDenied()
		}
		if err := validateOverwriteTarget(ctx, txStore, authority, channel.GuildID, req.GetTargetType(), req.GetTargetId()); err != nil {
			return err
		}
		return txStore.DeleteGuildChannelPermissionOverwrite(ctx, channel.ID, int32(req.GetTargetType()), req.GetTargetId())
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	event, eventErr := newGuildChannelOverwriteDeletedEvent(guildID, req.GetChannelId(), int32(req.GetTargetType()), req.GetTargetId())
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.DeleteGuildChannelPermissionOverwriteResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *guildServer) ListGuildChannelPermissionOverwrites(
	ctx context.Context,
	req *guildv1.ListGuildChannelPermissionOverwritesRequest,
) (*guildv1.ListGuildChannelPermissionOverwritesResponse, error) {
	if err := validateChannelActorRequest(req.GetChannelId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	channel, err := s.svcCtx.Store.GetGuildChannel(ctx, req.GetChannelId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	authority, err := loadMemberAuthority(ctx, s.svcCtx.Store, channel.GuildID, req.GetActorUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	if !authority.has(PermissionManageChannels) {
		return nil, permissionDenied()
	}
	overwrites, err := s.svcCtx.Store.ListGuildChannelPermissionOverwrites(ctx, channel.ID)
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp := new(guildv1.ListGuildChannelPermissionOverwritesResponse)
	resp.SetOverwrites(guildChannelOverwritesToProto(overwrites))
	return resp, nil
}

func (s *guildServer) AuthorizeGuildChannel(ctx context.Context, req *guildv1.AuthorizeGuildChannelRequest) (*guildv1.AuthorizeGuildChannelResponse, error) {
	if req.GetChannelId() <= 0 || req.GetUserId() <= 0 {
		return nil, invalidRequest("channel id and user id are required")
	}
	if req.GetPermission() == 0 {
		return nil, invalidRequest("permission is required")
	}
	if req.GetPermission()&^AllChannelPermissions != 0 {
		return nil, invalidRequest("permission contains non-channel bits")
	}
	channel, permissions, err := s.loadAuthorizedChannel(ctx, req.GetChannelId(), req.GetUserId())
	if err != nil {
		return nil, err
	}
	resp := new(guildv1.AuthorizeGuildChannelResponse)
	resp.SetGuildId(channel.GuildID)
	resp.SetPermissions(permissions)
	resp.SetAllowed(permissions&req.GetPermission() == req.GetPermission())
	return resp, nil
}

func (s *guildServer) loadAuthorizedChannel(ctx context.Context, channelID, userID int64) (*model.Channel, uint64, error) {
	channel, err := s.svcCtx.Store.GetGuildChannel(ctx, channelID)
	if err != nil {
		return nil, 0, mapStoreError(err)
	}
	authority, err := loadMemberAuthority(ctx, s.svcCtx.Store, channel.GuildID, userID)
	if err != nil {
		return nil, 0, mapStoreError(err)
	}
	roles, err := s.svcCtx.Store.ListGuildMemberRoles(ctx, channel.GuildID, userID)
	if err != nil {
		return nil, 0, mapStoreError(err)
	}
	overwrites, err := s.svcCtx.Store.ListGuildChannelPermissionOverwrites(ctx, channelID)
	if err != nil {
		return nil, 0, mapStoreError(err)
	}
	return channel, channelPermissions(authority, roles, overwrites, userID), nil
}

func validateOverwriteTarget(
	ctx context.Context,
	guildStore store.Store,
	authority memberAuthority,
	guildID int64,
	targetType guildv1.GuildPermissionOverwriteType,
	targetID int64,
) error {
	switch targetType {
	case guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE:
		role, err := guildStore.GetGuildRole(ctx, guildID, targetID)
		if err != nil {
			return err
		}
		if !role.IsDefault && !authority.canManageRole(role) {
			return permissionDenied()
		}
	case guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER:
		target, err := loadMemberAuthority(ctx, guildStore, guildID, targetID)
		if err != nil {
			return err
		}
		if !canManageMember(authority, target) {
			return permissionDenied()
		}
	default:
		return invalidRequest("invalid overwrite target type")
	}
	return nil
}

func validateOverwriteRequest(
	channelID, actorUserID int64,
	targetType guildv1.GuildPermissionOverwriteType,
	targetID int64,
	allow, deny uint64,
) error {
	if err := validateChannelActorRequest(channelID, actorUserID); err != nil {
		return err
	}
	if targetID <= 0 {
		return invalidRequest("overwrite target id is required")
	}
	if targetType != guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE &&
		targetType != guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER {
		return invalidRequest("invalid overwrite target type")
	}
	if allow&deny != 0 {
		return invalidRequest("overwrite allow and deny must not overlap")
	}
	if (allow|deny)&^AllChannelPermissions != 0 {
		return invalidRequest("overwrite contains non-channel permission bits")
	}
	return nil
}

func validateChannelActorRequest(channelID, actorUserID int64) error {
	if channelID <= 0 {
		return invalidRequest("channel id is required")
	}
	if actorUserID <= 0 {
		return invalidRequest("actor user id is required")
	}
	return nil
}

func normalizeChannelName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", invalidRequest("channel name is required")
	}
	if utf8.RuneCountInString(name) > maxChannelNameRunes {
		return "", invalidRequest("channel name is too long")
	}
	return name, nil
}

func validateChannelTopic(topic string) error {
	if utf8.RuneCountInString(topic) > maxChannelTopicRunes {
		return invalidRequest("channel topic is too long")
	}
	return nil
}

func normalizeChannelType(value guildv1.GuildChannelType) (guildv1.GuildChannelType, error) {
	if value == guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_UNSPECIFIED {
		return guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT, nil
	}
	if value != guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT {
		return 0, invalidRequest("unsupported channel type")
	}
	return value, nil
}

func validateChannelPositions(channels []*model.Channel, positions map[int64]int32) error {
	final := make(map[int32]int64, len(channels))
	for _, channel := range channels {
		position := channel.Position
		if requested, ok := positions[channel.ID]; ok {
			position = requested
		}
		if existing, exists := final[position]; exists && existing != channel.ID {
			return invalidRequest("channel positions conflict")
		}
		final[position] = channel.ID
	}
	return nil
}

func guildChannelToProto(channel *model.Channel) *guildv1.GuildChannel {
	if channel == nil {
		return nil
	}
	value := new(guildv1.GuildChannel)
	value.SetId(channel.ID)
	value.SetGuildId(channel.GuildID)
	value.SetName(channel.Name)
	value.SetType(guildv1.GuildChannelType(channel.Type))
	value.SetPosition(channel.Position)
	value.SetTopic(channel.Topic)
	value.SetRevision(channel.Revision)
	value.SetCreatedAt(channel.CreatedAt)
	value.SetUpdatedAt(channel.UpdatedAt)
	return value
}

func guildChannelsToProto(channels []*model.Channel) []*guildv1.GuildChannel {
	values := make([]*guildv1.GuildChannel, 0, len(channels))
	for _, channel := range channels {
		values = append(values, guildChannelToProto(channel))
	}
	return values
}

func guildChannelOverwriteToProto(overwrite *model.ChannelPermissionOverwrite) *guildv1.GuildChannelPermissionOverwrite {
	if overwrite == nil {
		return nil
	}
	value := new(guildv1.GuildChannelPermissionOverwrite)
	value.SetChannelId(overwrite.ChannelID)
	value.SetGuildId(overwrite.GuildID)
	value.SetTargetType(guildv1.GuildPermissionOverwriteType(overwrite.TargetType))
	value.SetTargetId(overwrite.TargetID)
	value.SetAllow(overwrite.Allow)
	value.SetDeny(overwrite.Deny)
	value.SetRevision(overwrite.Revision)
	value.SetCreatedAt(overwrite.CreatedAt)
	value.SetUpdatedAt(overwrite.UpdatedAt)
	return value
}

func guildChannelOverwritesToProto(overwrites []*model.ChannelPermissionOverwrite) []*guildv1.GuildChannelPermissionOverwrite {
	values := make([]*guildv1.GuildChannelPermissionOverwrite, 0, len(overwrites))
	for _, overwrite := range overwrites {
		values = append(values, guildChannelOverwriteToProto(overwrite))
	}
	return values
}
