package store

import (
	"context"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

type roleRow struct {
	ID          int64  `db:"id"`
	GuildID     int64  `db:"guild_id"`
	Name        string `db:"name"`
	Permissions int64  `db:"permissions"`
	Position    int32  `db:"position"`
	IsDefault   bool   `db:"is_default"`
	Revision    int64  `db:"revision"`
	CreatedAt   int64  `db:"created_at"`
	UpdatedAt   int64  `db:"updated_at"`
	DeletedAt   int64  `db:"deleted_at"`
}

func (s *SQLStore) CreateGuildRole(
	ctx context.Context,
	roleID, guildID int64,
	name string,
	permissions uint64,
	position int32,
	createdAt int64,
) (*model.Role, error) {
	row := new(roleRow)
	if err := sqlx.GetContext(ctx, s.q, row, createGuildRoleQuery, roleID, guildID, name, int64(permissions), position, createdAt); err != nil {
		return nil, err
	}
	return roleFromRow(row), nil
}

func (s *SQLStore) GetGuildRole(ctx context.Context, guildID, roleID int64) (*model.Role, error) {
	row := new(roleRow)
	if err := sqlx.GetContext(ctx, s.q, row, getGuildRoleQuery, guildID, roleID); err != nil {
		return nil, err
	}
	return roleFromRow(row), nil
}

func (s *SQLStore) ListGuildRoles(ctx context.Context, guildID int64) ([]*model.Role, error) {
	var rows []*roleRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, listGuildRolesQuery, guildID); err != nil {
		return nil, err
	}
	roles := make([]*model.Role, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, roleFromRow(row))
	}
	return roles, nil
}

func (s *SQLStore) UpdateGuildRole(ctx context.Context, params UpdateGuildRoleParams) (*model.Role, error) {
	var name string
	if params.Name != nil {
		name = *params.Name
	}
	var permissions uint64
	if params.Permissions != nil {
		permissions = *params.Permissions
	}
	row := new(roleRow)
	if err := sqlx.GetContext(
		ctx,
		s.q,
		row,
		updateGuildRoleQuery,
		params.GuildID,
		params.RoleID,
		params.Name != nil,
		name,
		params.Permissions != nil,
		int64(permissions),
		params.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return roleFromRow(row), nil
}

func (s *SQLStore) UpdateGuildRolePosition(
	ctx context.Context,
	guildID, roleID int64,
	position int32,
	updatedAt int64,
) (*model.Role, error) {
	row := new(roleRow)
	if err := sqlx.GetContext(ctx, s.q, row, updateGuildRolePositionQuery, guildID, roleID, position, updatedAt); err != nil {
		return nil, err
	}
	return roleFromRow(row), nil
}

func (s *SQLStore) UpdateGuildRolePositions(ctx context.Context, guildID int64, roleIDs []int64, positions []int32, updatedAt int64) ([]*model.Role, error) {
	if len(roleIDs) == 0 || len(roleIDs) != len(positions) {
		return nil, nil
	}
	var rows []*roleRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, updateGuildRolePositionsQuery, guildID, pq.Array(roleIDs), pq.Array(positions), updatedAt); err != nil {
		return nil, err
	}
	roles := make([]*model.Role, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, roleFromRow(row))
	}
	return roles, nil
}

func (s *SQLStore) DeleteGuildRole(ctx context.Context, guildID, roleID, deletedAt int64) (*model.Role, error) {
	row := new(roleRow)
	if err := sqlx.GetContext(ctx, s.q, row, deleteGuildRoleQuery, guildID, roleID, deletedAt); err != nil {
		return nil, err
	}
	return roleFromRow(row), nil
}

func (s *SQLStore) AddGuildMemberRole(ctx context.Context, guildID, userID, roleID, createdAt int64) error {
	_, err := s.q.ExecContext(ctx, addGuildMemberRoleStatement, guildID, userID, roleID, createdAt)
	return err
}

func (s *SQLStore) RemoveGuildMemberRole(ctx context.Context, guildID, userID, roleID int64) error {
	_, err := s.q.ExecContext(ctx, removeGuildMemberRoleStatement, guildID, userID, roleID)
	return err
}

func (s *SQLStore) DeleteGuildMemberRoleAssignments(ctx context.Context, guildID, userID int64) error {
	_, err := s.q.ExecContext(ctx, deleteGuildMemberRoleAssignmentsStatement, guildID, userID)
	return err
}

func (s *SQLStore) DeleteGuildRoleAssignments(ctx context.Context, guildID, roleID int64) error {
	_, err := s.q.ExecContext(ctx, deleteGuildRoleAssignmentsStatement, guildID, roleID)
	return err
}

func (s *SQLStore) DeleteAllGuildRoleAssignments(ctx context.Context, guildID int64) error {
	_, err := s.q.ExecContext(ctx, deleteAllGuildRoleAssignmentsStatement, guildID)
	return err
}

func (s *SQLStore) ListGuildMemberRoles(ctx context.Context, guildID, userID int64) ([]*model.Role, error) {
	var rows []*roleRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, listGuildMemberRolesQuery, guildID, userID); err != nil {
		return nil, err
	}
	roles := make([]*model.Role, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, roleFromRow(row))
	}
	return roles, nil
}

func (s *SQLStore) ListGuildMemberRolesByGuilds(ctx context.Context, guildIDs []int64, userID int64) ([]*model.Role, error) {
	if len(guildIDs) == 0 {
		return nil, nil
	}
	var rows []*roleRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, listGuildMemberRolesByGuildsQuery, pq.Array(guildIDs), userID); err != nil {
		return nil, err
	}
	roles := make([]*model.Role, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, roleFromRow(row))
	}
	return roles, nil
}

func roleFromRow(row *roleRow) *model.Role {
	return &model.Role{
		ID:          row.ID,
		GuildID:     row.GuildID,
		Name:        row.Name,
		Permissions: uint64(row.Permissions),
		Position:    row.Position,
		IsDefault:   row.IsDefault,
		Revision:    row.Revision,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
		DeletedAt:   row.DeletedAt,
	}
}
