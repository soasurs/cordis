package server

import (
	"context"
	"database/sql"
	"sort"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

func (s *fakeStore) CreateGuildRole(
	_ context.Context,
	roleID, guildID int64,
	name string,
	permissions uint64,
	position int32,
	createdAt int64,
) (*model.Role, error) {
	if s.roles[guildID] == nil {
		s.roles[guildID] = make(map[int64]*model.Role)
	}
	role := &model.Role{
		ID: roleID, GuildID: guildID, Name: name, Permissions: permissions,
		Position: position, Revision: 1, CreatedAt: createdAt,
	}
	s.roles[guildID][roleID] = role
	return cloneRole(role), nil
}

func (s *fakeStore) GetGuildRole(_ context.Context, guildID, roleID int64) (*model.Role, error) {
	role := s.roles[guildID][roleID]
	if role == nil || role.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	return cloneRole(role), nil
}

func (s *fakeStore) ListGuildRoles(_ context.Context, guildID int64) ([]*model.Role, error) {
	var roles []*model.Role
	for _, role := range s.roles[guildID] {
		if role.DeletedAt == 0 {
			roles = append(roles, cloneRole(role))
		}
	}
	sort.Slice(roles, func(i, j int) bool {
		if roles[i].Position == roles[j].Position {
			return roles[i].ID < roles[j].ID
		}
		return roles[i].Position > roles[j].Position
	})
	return roles, nil
}

func (s *fakeStore) ListGuildRolesByGuilds(ctx context.Context, guildIDs []int64) ([]*model.Role, error) {
	var roles []*model.Role
	for _, guildID := range guildIDs {
		values, err := s.ListGuildRoles(ctx, guildID)
		if err != nil {
			return nil, err
		}
		roles = append(roles, values...)
	}
	return roles, nil
}

func (s *fakeStore) UpdateGuildRole(_ context.Context, params store.UpdateGuildRoleParams) (*model.Role, error) {
	role := s.roles[params.GuildID][params.RoleID]
	if role == nil || role.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	if params.Name != nil {
		role.Name = *params.Name
	}
	if params.Permissions != nil {
		role.Permissions = *params.Permissions
	}
	role.Revision++
	role.UpdatedAt = params.UpdatedAt
	return cloneRole(role), nil
}

func (s *fakeStore) UpdateGuildRolePosition(_ context.Context, guildID, roleID int64, position int32, updatedAt int64) (*model.Role, error) {
	role := s.roles[guildID][roleID]
	if role == nil || role.DeletedAt != 0 || role.IsDefault {
		return nil, sql.ErrNoRows
	}
	role.Position = position
	role.Revision++
	role.UpdatedAt = updatedAt
	return cloneRole(role), nil
}

func (s *fakeStore) UpdateGuildRolePositions(ctx context.Context, guildID int64, roleIDs []int64, positions []int32, updatedAt int64) ([]*model.Role, error) {
	roles := make([]*model.Role, 0, len(roleIDs))
	for i, roleID := range roleIDs {
		role, err := s.UpdateGuildRolePosition(ctx, guildID, roleID, positions[i], updatedAt)
		if err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, nil
}

func (s *fakeStore) DeleteGuildRole(_ context.Context, guildID, roleID, deletedAt int64) (*model.Role, error) {
	role := s.roles[guildID][roleID]
	if role == nil || role.DeletedAt != 0 || role.IsDefault {
		return nil, sql.ErrNoRows
	}
	role.Revision++
	role.UpdatedAt = deletedAt
	role.DeletedAt = deletedAt
	return cloneRole(role), nil
}

func (s *fakeStore) AddGuildMemberRole(_ context.Context, guildID, userID, roleID, _ int64) error {
	if s.memberRoles[guildID] == nil {
		s.memberRoles[guildID] = make(map[int64]map[int64]bool)
	}
	if s.memberRoles[guildID][userID] == nil {
		s.memberRoles[guildID][userID] = make(map[int64]bool)
	}
	s.memberRoles[guildID][userID][roleID] = true
	return nil
}

func (s *fakeStore) RemoveGuildMemberRole(_ context.Context, guildID, userID, roleID int64) error {
	delete(s.memberRoles[guildID][userID], roleID)
	return nil
}

func (s *fakeStore) DeleteGuildMemberRoleAssignments(_ context.Context, guildID, userID int64) error {
	delete(s.memberRoles[guildID], userID)
	return nil
}

func (s *fakeStore) DeleteGuildRoleAssignments(_ context.Context, guildID, roleID int64) error {
	for _, roles := range s.memberRoles[guildID] {
		delete(roles, roleID)
	}
	return nil
}

func (s *fakeStore) DeleteAllGuildRoleAssignments(_ context.Context, guildID int64) error {
	delete(s.memberRoles, guildID)
	return nil
}

func (s *fakeStore) ListGuildMemberRoles(_ context.Context, guildID, userID int64) ([]*model.Role, error) {
	member := s.members[guildID][userID]
	if member == nil || member.DeletedAt != 0 {
		return nil, nil
	}
	var roles []*model.Role
	for _, role := range s.roles[guildID] {
		if role.DeletedAt != 0 {
			continue
		}
		if role.IsDefault || s.memberRoles[guildID][userID][role.ID] {
			roles = append(roles, cloneRole(role))
		}
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Position > roles[j].Position })
	return roles, nil
}

func (s *fakeStore) ListGuildMemberRolesByGuilds(ctx context.Context, guildIDs []int64, userID int64) ([]*model.Role, error) {
	var roles []*model.Role
	for _, guildID := range guildIDs {
		values, err := s.ListGuildMemberRoles(ctx, guildID, userID)
		if err != nil {
			return nil, err
		}
		roles = append(roles, values...)
	}
	return roles, nil
}
