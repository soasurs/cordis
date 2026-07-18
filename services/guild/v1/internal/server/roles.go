package server

import (
	"context"
	"math"
	"time"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

func (s *guildServer) CreateGuildRole(ctx context.Context, req *guildv1.CreateGuildRoleRequest) (*guildv1.CreateGuildRoleResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	name, err := normalizeRoleName(req.GetName())
	if err != nil {
		return nil, err
	}
	if err := validatePermissions(req.GetPermissions()); err != nil {
		return nil, err
	}

	var role *model.Role
	createdAt := time.Now().UnixMilli()
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		authority, err := loadMemberAuthority(ctx, txStore, req.GetGuildId(), req.GetActorUserId())
		if err != nil {
			return err
		}
		if !authority.has(PermissionManageRoles) {
			return permissionDenied()
		}
		if !authority.canGrantPermissions(req.GetPermissions()) {
			return permissionDenied()
		}
		if err := txStore.CheckResourceQuota(ctx, store.ResourceQuota{
			Kind: store.QuotaGuildRoles, ScopeID: req.GetGuildId(), Limit: s.svcCtx.Cfg.Limits.Roles(),
		}); err != nil {
			return err
		}
		roles, err := txStore.ListGuildRoles(ctx, req.GetGuildId())
		if err != nil {
			return err
		}
		position := nextRolePosition(roles)
		if !authority.IsOwner {
			// New roles created by delegated managers must start below the
			// manager's highest role so creation cannot grant upward control.
			position = nextManagedRolePosition(roles, authority.HighestRole)
			if position <= 0 {
				return permissionDenied()
			}
		}
		role, err = txStore.CreateGuildRole(
			ctx,
			s.svcCtx.Snowflake.Generate().Int64(),
			req.GetGuildId(),
			name,
			req.GetPermissions(),
			position,
			createdAt,
		)
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	event, eventErr := newGuildRoleCreatedEvent(role)
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.CreateGuildRoleResponse)
	resp.SetRole(guildRoleToProto(role))
	return resp, nil
}

func (s *guildServer) GetGuildRole(ctx context.Context, req *guildv1.GetGuildRoleRequest) (*guildv1.GetGuildRoleResponse, error) {
	if err := validateRoleRequest(req.GetGuildId(), req.GetActorUserId(), req.GetRoleId()); err != nil {
		return nil, err
	}
	if _, err := s.svcCtx.Store.GetGuildForMember(ctx, req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, mapStoreError(err)
	}
	role, err := s.svcCtx.Store.GetGuildRole(ctx, req.GetGuildId(), req.GetRoleId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp := new(guildv1.GetGuildRoleResponse)
	resp.SetRole(guildRoleToProto(role))
	return resp, nil
}

func (s *guildServer) ListGuildRoles(ctx context.Context, req *guildv1.ListGuildRolesRequest) (*guildv1.ListGuildRolesResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if _, err := s.svcCtx.Store.GetGuildForMember(ctx, req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, mapStoreError(err)
	}
	roles, err := s.svcCtx.Store.ListGuildRoles(ctx, req.GetGuildId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp := new(guildv1.ListGuildRolesResponse)
	resp.SetRoles(guildRolesToProto(roles))
	return resp, nil
}

func (s *guildServer) UpdateGuildRole(ctx context.Context, req *guildv1.UpdateGuildRoleRequest) (*guildv1.UpdateGuildRoleResponse, error) {
	if err := validateRoleRequest(req.GetGuildId(), req.GetActorUserId(), req.GetRoleId()); err != nil {
		return nil, err
	}
	params := store.UpdateGuildRoleParams{GuildID: req.GetGuildId(), RoleID: req.GetRoleId(), UpdatedAt: time.Now().UnixMilli()}
	if req.HasName() {
		name, err := normalizeRoleName(req.GetName())
		if err != nil {
			return nil, err
		}
		params.Name = &name
	}
	if req.HasPermissions() {
		if err := validatePermissions(req.GetPermissions()); err != nil {
			return nil, err
		}
		permissions := req.GetPermissions()
		params.Permissions = &permissions
	}
	if params.Name == nil && params.Permissions == nil {
		return nil, invalidRequest("at least one role field is required")
	}

	var updated *model.Role
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		authority, err := loadMemberAuthority(ctx, txStore, req.GetGuildId(), req.GetActorUserId())
		if err != nil {
			return err
		}
		if !authority.has(PermissionManageRoles) {
			return permissionDenied()
		}
		if params.Permissions != nil && !authority.canGrantPermissions(*params.Permissions) {
			return permissionDenied()
		}
		role, err := txStore.GetGuildRole(ctx, req.GetGuildId(), req.GetRoleId())
		if err != nil {
			return err
		}
		if role.IsDefault {
			if params.Name != nil {
				return invalidRequest("default role name cannot be changed")
			}
		} else if !authority.canManageRole(role) {
			return permissionDenied()
		}
		updated, err = txStore.UpdateGuildRole(ctx, params)
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	event, eventErr := newGuildRoleUpdatedEvent(updated)
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.UpdateGuildRoleResponse)
	resp.SetRole(guildRoleToProto(updated))
	return resp, nil
}

func (s *guildServer) DeleteGuildRole(ctx context.Context, req *guildv1.DeleteGuildRoleRequest) (*guildv1.DeleteGuildRoleResponse, error) {
	if err := validateRoleRequest(req.GetGuildId(), req.GetActorUserId(), req.GetRoleId()); err != nil {
		return nil, err
	}
	var deleted *model.Role
	deletedAt := time.Now().UnixMilli()
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		authority, err := loadMemberAuthority(ctx, txStore, req.GetGuildId(), req.GetActorUserId())
		if err != nil {
			return err
		}
		if !authority.has(PermissionManageRoles) {
			return permissionDenied()
		}
		role, err := txStore.GetGuildRole(ctx, req.GetGuildId(), req.GetRoleId())
		if err != nil {
			return err
		}
		if role.IsDefault {
			return invalidRequest("default role cannot be deleted")
		}
		if !authority.canManageRole(role) {
			return permissionDenied()
		}
		if err := txStore.DeleteGuildRoleAssignments(ctx, req.GetGuildId(), req.GetRoleId()); err != nil {
			return err
		}
		if err := txStore.DeleteGuildChannelPermissionOverwritesForTarget(
			ctx,
			req.GetGuildId(),
			int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE),
			req.GetRoleId(),
		); err != nil {
			return err
		}
		deleted, err = txStore.DeleteGuildRole(ctx, req.GetGuildId(), req.GetRoleId(), deletedAt)
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	event, eventErr := newGuildRoleDeletedEvent(deleted)
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.DeleteGuildRoleResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *guildServer) ReorderGuildRoles(ctx context.Context, req *guildv1.ReorderGuildRolesRequest) (*guildv1.ReorderGuildRolesResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if len(req.GetPositions()) == 0 {
		return nil, invalidRequest("role positions are required")
	}
	positions := make(map[int64]int32, len(req.GetPositions()))
	usedPositions := make(map[int32]struct{}, len(req.GetPositions()))
	for _, item := range req.GetPositions() {
		if item.GetRoleId() <= 0 || item.GetPosition() <= 0 {
			return nil, invalidRequest("role id and position must be positive")
		}
		if _, exists := positions[item.GetRoleId()]; exists {
			return nil, invalidRequest("role id must be unique")
		}
		if _, exists := usedPositions[item.GetPosition()]; exists {
			return nil, invalidRequest("role position must be unique")
		}
		positions[item.GetRoleId()] = item.GetPosition()
		usedPositions[item.GetPosition()] = struct{}{}
	}

	var reordered []*model.Role
	var roles []*model.Role
	updatedAt := time.Now().UnixMilli()
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		authority, err := loadMemberAuthority(ctx, txStore, req.GetGuildId(), req.GetActorUserId())
		if err != nil {
			return err
		}
		if !authority.has(PermissionManageRoles) {
			return permissionDenied()
		}
		currentRoles, err := txStore.ListGuildRoles(ctx, req.GetGuildId())
		if err != nil {
			return err
		}
		finalPositions := make(map[int32]int64, len(currentRoles))
		for _, role := range currentRoles {
			if role.IsDefault {
				continue
			}
			position := role.Position
			if requested, ok := positions[role.ID]; ok {
				position = requested
			}
			if existingRoleID, exists := finalPositions[position]; exists && existingRoleID != role.ID {
				return invalidRequest("role positions conflict with existing roles")
			}
			finalPositions[position] = role.ID
		}
		for roleID, position := range positions {
			role, err := txStore.GetGuildRole(ctx, req.GetGuildId(), roleID)
			if err != nil {
				return err
			}
			if role.IsDefault || !authority.canManageRole(role) {
				return permissionDenied()
			}
			if !authority.IsOwner && position >= authority.HighestRole {
				return permissionDenied()
			}
			updated, err := txStore.UpdateGuildRolePosition(ctx, req.GetGuildId(), roleID, position, updatedAt)
			if err != nil {
				return err
			}
			reordered = append(reordered, updated)
		}
		roles, err = txStore.ListGuildRoles(ctx, req.GetGuildId())
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	for _, role := range reordered {
		event, eventErr := newGuildRoleUpdatedEvent(role)
		s.publishEvent(ctx, event, eventErr)
	}
	resp := new(guildv1.ReorderGuildRolesResponse)
	resp.SetRoles(guildRolesToProto(roles))
	return resp, nil
}

func (s *guildServer) AddGuildMemberRole(ctx context.Context, req *guildv1.AddGuildMemberRoleRequest) (*guildv1.AddGuildMemberRoleResponse, error) {
	if err := validateMemberRoleRequest(req.GetGuildId(), req.GetActorUserId(), req.GetUserId(), req.GetRoleId()); err != nil {
		return nil, err
	}
	if err := s.changeGuildMemberRole(ctx, req.GetGuildId(), req.GetActorUserId(), req.GetUserId(), req.GetRoleId(), true); err != nil {
		return nil, err
	}
	resp := new(guildv1.AddGuildMemberRoleResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *guildServer) RemoveGuildMemberRole(ctx context.Context, req *guildv1.RemoveGuildMemberRoleRequest) (*guildv1.RemoveGuildMemberRoleResponse, error) {
	if err := validateMemberRoleRequest(req.GetGuildId(), req.GetActorUserId(), req.GetUserId(), req.GetRoleId()); err != nil {
		return nil, err
	}
	if err := s.changeGuildMemberRole(ctx, req.GetGuildId(), req.GetActorUserId(), req.GetUserId(), req.GetRoleId(), false); err != nil {
		return nil, err
	}
	resp := new(guildv1.RemoveGuildMemberRoleResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *guildServer) changeGuildMemberRole(
	ctx context.Context,
	guildID, actorUserID, userID, roleID int64,
	add bool,
) error {
	changedAt := time.Now().UnixMilli()
	var roles []*model.Role
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		actor, err := loadMemberAuthority(ctx, txStore, guildID, actorUserID)
		if err != nil {
			return err
		}
		if !actor.has(PermissionManageRoles) {
			return permissionDenied()
		}
		target, err := loadMemberAuthority(ctx, txStore, guildID, userID)
		if err != nil {
			return err
		}
		role, err := txStore.GetGuildRole(ctx, guildID, roleID)
		if err != nil {
			return err
		}
		if role.IsDefault {
			return invalidRequest("default role is assigned implicitly")
		}
		if !actor.canManageRole(role) || !canManageMember(actor, target) {
			return permissionDenied()
		}
		if add {
			if !actor.canGrantPermissions(role.Permissions) {
				return permissionDenied()
			}
			if err := txStore.AddGuildMemberRole(ctx, guildID, userID, roleID, changedAt); err != nil {
				return err
			}
		} else if err := txStore.RemoveGuildMemberRole(ctx, guildID, userID, roleID); err != nil {
			return err
		}
		roles, err = txStore.ListGuildMemberRoles(ctx, guildID, userID)
		return err
	})
	if err != nil {
		return mapStoreError(err)
	}
	event, eventErr := newGuildMemberRolesUpdatedEvent(guildID, userID, roles, changedAt)
	s.publishEvent(ctx, event, eventErr)
	return nil
}

func (s *guildServer) ListGuildMemberRoles(ctx context.Context, req *guildv1.ListGuildMemberRolesRequest) (*guildv1.ListGuildMemberRolesResponse, error) {
	if err := validateMemberRoleRequest(req.GetGuildId(), req.GetActorUserId(), req.GetUserId(), 1); err != nil {
		return nil, err
	}
	if _, err := s.svcCtx.Store.GetGuildForMember(ctx, req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, mapStoreError(err)
	}
	if _, err := s.svcCtx.Store.GetGuildMember(ctx, req.GetGuildId(), req.GetUserId()); err != nil {
		return nil, mapStoreError(err)
	}
	roles, err := s.svcCtx.Store.ListGuildMemberRoles(ctx, req.GetGuildId(), req.GetUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp := new(guildv1.ListGuildMemberRolesResponse)
	resp.SetRoles(guildRolesToProto(roles))
	return resp, nil
}

func (s *guildServer) GetGuildMemberPermissions(ctx context.Context, req *guildv1.GetGuildMemberPermissionsRequest) (*guildv1.GetGuildMemberPermissionsResponse, error) {
	if err := validateMemberRoleRequest(req.GetGuildId(), req.GetActorUserId(), req.GetUserId(), 1); err != nil {
		return nil, err
	}
	if _, err := s.svcCtx.Store.GetGuildForMember(ctx, req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, mapStoreError(err)
	}
	authority, err := loadMemberAuthority(ctx, s.svcCtx.Store, req.GetGuildId(), req.GetUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp := new(guildv1.GetGuildMemberPermissionsResponse)
	resp.SetPermissions(authority.Permissions)
	return resp, nil
}

func validateRoleRequest(guildID, actorUserID, roleID int64) error {
	if err := validateMemberActorRequest(guildID, actorUserID); err != nil {
		return err
	}
	if roleID <= 0 {
		return invalidRequest("role id is required")
	}
	return nil
}

func validateMemberRoleRequest(guildID, actorUserID, userID, roleID int64) error {
	if err := validateRoleRequest(guildID, actorUserID, roleID); err != nil {
		return err
	}
	if userID <= 0 {
		return invalidRequest("user id is required")
	}
	return nil
}

func validatePermissions(permissions uint64) error {
	if permissions > math.MaxInt64 {
		return invalidRequest("permissions exceed storage range")
	}
	if permissions&^AllGuildPermissions != 0 {
		return invalidRequest("permissions contain unsupported bits")
	}
	return nil
}

func nextRolePosition(roles []*model.Role) int32 {
	var highest int32
	for _, role := range roles {
		if role.Position > highest {
			highest = role.Position
		}
	}
	if highest == math.MaxInt32 {
		return math.MaxInt32
	}
	return highest + 1
}

func nextManagedRolePosition(roles []*model.Role, actorHighest int32) int32 {
	used := make(map[int32]struct{}, len(roles))
	for _, role := range roles {
		used[role.Position] = struct{}{}
	}
	for position := actorHighest - 1; position > 0; position-- {
		if _, exists := used[position]; !exists {
			return position
		}
	}
	return 0
}

func guildRoleToProto(role *model.Role) *guildv1.GuildRole {
	if role == nil {
		return nil
	}
	value := new(guildv1.GuildRole)
	value.SetId(role.ID)
	value.SetGuildId(role.GuildID)
	value.SetName(role.Name)
	value.SetPermissions(role.Permissions)
	value.SetPosition(role.Position)
	value.SetIsDefault(role.IsDefault)
	value.SetRevision(role.Revision)
	value.SetCreatedAt(role.CreatedAt)
	value.SetUpdatedAt(role.UpdatedAt)
	return value
}

func guildRolesToProto(roles []*model.Role) []*guildv1.GuildRole {
	values := make([]*guildv1.GuildRole, 0, len(roles))
	for _, role := range roles {
		values = append(values, guildRoleToProto(role))
	}
	return values
}
