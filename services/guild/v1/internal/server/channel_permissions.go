package server

import (
	"context"
	"time"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

// UpsertGuildChannelPermissionOverwrite creates or replaces one channel
// overwrite when the actor can manage channels and grant the requested bits.
func (s *guildServer) UpsertGuildChannelPermissionOverwrite(
	ctx context.Context,
	req *guildv1.UpsertGuildChannelPermissionOverwriteRequest,
) (*guildv1.UpsertGuildChannelPermissionOverwriteResponse, error) {
	if err := validateOverwriteRequest(req.GetChannelId(), req.GetActorUserId(), req.GetAppliesTo(), req.GetAppliesToId(), req.GetAllow(), req.GetDeny()); err != nil {
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
		if err := validateOverwriteAppliesTo(ctx, txStore, authority, channel.GuildID, req.GetAppliesTo(), req.GetAppliesToId()); err != nil {
			return err
		}
		if err := txStore.CheckResourceQuota(ctx, store.ResourceQuota{
			Kind: store.QuotaChannelOverwrites, ScopeID: channel.ID, Limit: s.svcCtx.Cfg.Limits.Overwrites(),
			AppliesTo: int32(req.GetAppliesTo()), AppliesToID: req.GetAppliesToId(),
		}); err != nil {
			return err
		}
		overwrite, err = txStore.UpsertGuildChannelPermissionOverwrite(ctx, &model.ChannelPermissionOverwrite{
			ChannelID: channel.ID, GuildID: channel.GuildID, AppliesTo: int32(req.GetAppliesTo()),
			AppliesToID: req.GetAppliesToId(), Allow: req.GetAllow(), Deny: req.GetDeny(), CreatedAt: changedAt,
		})
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	event, eventErr := newGuildChannelOverwriteUpdatedEvent(overwrite, s.svcCtx.Snowflake.Generate().Int64())
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.UpsertGuildChannelPermissionOverwriteResponse)
	resp.SetOverwrite(guildChannelOverwriteToProto(overwrite))
	return resp, nil
}

// DeleteGuildChannelPermissionOverwrite removes one channel overwrite,
// rejecting deletion of the default @everyone overwrite.
func (s *guildServer) DeleteGuildChannelPermissionOverwrite(
	ctx context.Context,
	req *guildv1.DeleteGuildChannelPermissionOverwriteRequest,
) (*guildv1.DeleteGuildChannelPermissionOverwriteResponse, error) {
	if err := validateOverwriteRequest(req.GetChannelId(), req.GetActorUserId(), req.GetAppliesTo(), req.GetAppliesToId(), 0, 0); err != nil {
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
		if isDefaultEveryoneOverwrite(req.GetAppliesTo(), req.GetAppliesToId(), channel.GuildID) {
			return invalidRequest("default role overwrite cannot be deleted")
		}
		if err := validateOverwriteAppliesTo(ctx, txStore, authority, channel.GuildID, req.GetAppliesTo(), req.GetAppliesToId()); err != nil {
			return err
		}
		return txStore.DeleteGuildChannelPermissionOverwrite(ctx, channel.ID, int32(req.GetAppliesTo()), req.GetAppliesToId())
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	event, eventErr := newGuildChannelOverwriteDeletedEvent(guildID, req.GetChannelId(), int32(req.GetAppliesTo()), req.GetAppliesToId(), s.svcCtx.Snowflake.Generate().Int64())
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.DeleteGuildChannelPermissionOverwriteResponse)
	resp.SetOk(true)
	return resp, nil
}

// ListGuildChannelPermissionOverwrites returns the overwrites of one channel
// for an actor with MANAGE_CHANNELS.
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

// AuthorizeGuildChannel resolves one user's effective permissions on a
// channel.
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
	resp.SetChannelType(guildv1.GuildChannelType(channel.Type))
	return resp, nil
}

func (s *guildServer) loadAuthorizedChannel(ctx context.Context, channelID, userID int64) (*model.Channel, uint64, error) {
	channel, err := s.svcCtx.Store.GetGuildChannel(ctx, channelID)
	if err != nil {
		return nil, 0, mapStoreError(err)
	}
	authority, roles, err := loadMemberAuthorityAndRoles(ctx, s.svcCtx.Store, channel.GuildID, userID)
	if err != nil {
		return nil, 0, mapStoreError(err)
	}
	if authority.IsOwner || authority.Permissions&PermissionAdministrator != 0 {
		return channel, AllGuildPermissions, nil
	}
	overwrites, err := s.svcCtx.Store.ListGuildChannelPermissionOverwrites(ctx, channelID)
	if err != nil {
		return nil, 0, mapStoreError(err)
	}
	return channel, channelPermissions(authority, roles, overwrites, userID), nil
}

func upsertDefaultEveryoneOverwrite(
	ctx context.Context,
	txStore store.Store,
	channelID, guildID, createdAt int64,
	limit int,
) (*model.ChannelPermissionOverwrite, error) {
	appliesTo := int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE)
	if err := txStore.CheckResourceQuota(ctx, store.ResourceQuota{
		Kind: store.QuotaChannelOverwrites, ScopeID: channelID, Limit: limit,
		AppliesTo: appliesTo, AppliesToID: guildID,
	}); err != nil {
		return nil, err
	}
	// Empty allow/deny keeps channel permissions equal to the guild baseline until
	// an explicit overwrite change is upserted. The row still exists so clients
	// always receive the @everyone entry without synthesizing it.
	return txStore.UpsertGuildChannelPermissionOverwrite(ctx, &model.ChannelPermissionOverwrite{
		ChannelID: channelID, GuildID: guildID, AppliesTo: appliesTo, AppliesToID: guildID,
		CreatedAt: createdAt,
	})
}

func isDefaultEveryoneOverwrite(appliesTo guildv1.GuildPermissionOverwriteType, appliesToID, guildID int64) bool {
	return appliesTo == guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE &&
		appliesToID == guildID
}

func validateOverwriteAppliesTo(
	ctx context.Context,
	guildStore store.Store,
	authority memberAuthority,
	guildID int64,
	appliesTo guildv1.GuildPermissionOverwriteType,
	appliesToID int64,
) error {
	switch appliesTo {
	case guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE:
		role, err := guildStore.GetGuildRole(ctx, guildID, appliesToID)
		if err != nil {
			return err
		}
		if !role.IsDefault && !authority.canManageRole(role) {
			return permissionDenied()
		}
	case guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER:
		target, err := loadMemberAuthority(ctx, guildStore, guildID, appliesToID)
		if err != nil {
			return err
		}
		if !canManageMember(authority, target) {
			return permissionDenied()
		}
	default:
		return invalidRequest("invalid overwrite applies_to")
	}
	return nil
}

func validateOverwriteRequest(
	channelID, actorUserID int64,
	appliesTo guildv1.GuildPermissionOverwriteType,
	appliesToID int64,
	allow, deny uint64,
) error {
	if err := validateChannelActorRequest(channelID, actorUserID); err != nil {
		return err
	}
	if appliesToID <= 0 {
		return invalidRequest("overwrite applies_to_id is required")
	}
	if appliesTo != guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE &&
		appliesTo != guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER {
		return invalidRequest("invalid overwrite applies_to")
	}
	if allow&deny != 0 {
		return invalidRequest("overwrite allow and deny must not overlap")
	}
	if (allow|deny)&^AllChannelPermissions != 0 {
		return invalidRequest("overwrite contains non-channel permission bits")
	}
	return nil
}
