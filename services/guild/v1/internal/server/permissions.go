package server

import (
	"context"
	"math"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

const (
	PermissionAdministrator = uint64(guildv1.GuildPermission_GUILD_PERMISSION_ADMINISTRATOR)
	PermissionManageGuild   = uint64(guildv1.GuildPermission_GUILD_PERMISSION_MANAGE_GUILD)
	PermissionManageRoles   = uint64(guildv1.GuildPermission_GUILD_PERMISSION_MANAGE_ROLES)
	PermissionManageMembers = uint64(guildv1.GuildPermission_GUILD_PERMISSION_MANAGE_MEMBERS)
	PermissionKickMembers   = uint64(guildv1.GuildPermission_GUILD_PERMISSION_KICK_MEMBERS)
)

const AllGuildPermissions = PermissionAdministrator |
	PermissionManageGuild |
	PermissionManageRoles |
	PermissionManageMembers |
	PermissionKickMembers

type memberAuthority struct {
	Guild       *model.Guild
	Permissions uint64
	HighestRole int32
	IsOwner     bool
}

func loadMemberAuthority(
	ctx context.Context,
	guildStore store.Store,
	guildID, userID int64,
) (memberAuthority, error) {
	guild, err := guildStore.GetGuildForMember(ctx, guildID, userID)
	if err != nil {
		return memberAuthority{}, err
	}
	if guild.OwnerID == userID {
		return memberAuthority{
			Guild:       guild,
			Permissions: AllGuildPermissions,
			HighestRole: math.MaxInt32,
			IsOwner:     true,
		}, nil
	}

	roles, err := guildStore.ListGuildMemberRoles(ctx, guildID, userID)
	if err != nil {
		return memberAuthority{}, err
	}
	var permissions uint64
	var highestRole int32
	for _, role := range roles {
		permissions |= role.Permissions
		if !role.IsDefault && role.Position > highestRole {
			highestRole = role.Position
		}
	}
	if permissions&PermissionAdministrator != 0 {
		permissions = AllGuildPermissions
	}
	return memberAuthority{
		Guild:       guild,
		Permissions: permissions,
		HighestRole: highestRole,
	}, nil
}

func (authority memberAuthority) has(permission uint64) bool {
	return authority.IsOwner ||
		authority.Permissions&PermissionAdministrator != 0 ||
		authority.Permissions&permission == permission
}

func (authority memberAuthority) canManageRole(role *model.Role) bool {
	if authority.IsOwner {
		return true
	}
	// A role manager may only change roles strictly below their highest role.
	// Keeping equality forbidden prevents peers from modifying each other.
	return role != nil && !role.IsDefault && authority.HighestRole > role.Position
}

func (authority memberAuthority) canGrantPermissions(permissions uint64) bool {
	if authority.IsOwner || authority.Permissions&PermissionAdministrator != 0 {
		return true
	}
	// Manage Roles authorizes role operations, but it must not be usable to
	// manufacture permissions the actor does not already hold.
	return permissions&^authority.Permissions == 0
}

func canManageMember(actor memberAuthority, target memberAuthority) bool {
	if actor.IsOwner {
		return true
	}
	if target.IsOwner {
		return false
	}
	// Permissions decide what action is available; hierarchy decides who it
	// may affect. Requiring a strict ordering avoids same-level moderation.
	return actor.HighestRole > target.HighestRole
}
