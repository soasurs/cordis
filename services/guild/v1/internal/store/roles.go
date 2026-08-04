package store

import (
	"context"

	"github.com/jackc/pgx/v5"

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
	row, err := scanOne(ctx, s.q, createGuildRoleQuery, pgx.RowToStructByName[roleRow], roleID, guildID, name, int64(permissions), position, createdAt)
	if err != nil {
		return nil, err
	}
	return roleFromRow(&row), nil
}

func (s *SQLStore) GetGuildRole(ctx context.Context, guildID, roleID int64) (*model.Role, error) {
	row, err := scanOne(ctx, s.q, getGuildRoleQuery, pgx.RowToStructByName[roleRow], guildID, roleID)
	if err != nil {
		return nil, err
	}
	return roleFromRow(&row), nil
}

func (s *SQLStore) ListGuildRoles(ctx context.Context, guildID int64) ([]*model.Role, error) {
	rows, err := scanMany(ctx, s.q, listGuildRolesQuery, pgx.RowToStructByName[roleRow], guildID)
	if err != nil {
		return nil, err
	}
	roles := make([]*model.Role, 0, len(rows))
	for i := range rows {
		roles = append(roles, roleFromRow(&rows[i]))
	}
	return roles, nil
}

func (s *SQLStore) ListGuildRolesByGuilds(ctx context.Context, guildIDs []int64) ([]*model.Role, error) {
	if len(guildIDs) == 0 {
		return nil, nil
	}
	rows, err := scanMany(ctx, s.q, listGuildRolesByGuildsQuery, pgx.RowToStructByName[roleRow], guildIDs)
	if err != nil {
		return nil, err
	}
	roles := make([]*model.Role, 0, len(rows))
	for i := range rows {
		roles = append(roles, roleFromRow(&rows[i]))
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
	row, err := scanOne(
		ctx, s.q, updateGuildRoleQuery, pgx.RowToStructByName[roleRow],
		params.GuildID,
		params.RoleID,
		params.Name != nil,
		name,
		params.Permissions != nil,
		int64(permissions),
		params.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return roleFromRow(&row), nil
}

func (s *SQLStore) UpdateGuildRolePosition(
	ctx context.Context,
	guildID, roleID int64,
	position int32,
	updatedAt int64,
) (*model.Role, error) {
	row, err := scanOne(ctx, s.q, updateGuildRolePositionQuery, pgx.RowToStructByName[roleRow], guildID, roleID, position, updatedAt)
	if err != nil {
		return nil, err
	}
	return roleFromRow(&row), nil
}

func (s *SQLStore) UpdateGuildRolePositions(ctx context.Context, guildID int64, roleIDs []int64, positions []int32, updatedAt int64) ([]*model.Role, error) {
	if len(roleIDs) == 0 || len(roleIDs) != len(positions) {
		return nil, nil
	}
	rows, err := scanMany(ctx, s.q, updateGuildRolePositionsQuery, pgx.RowToStructByName[roleRow], guildID, roleIDs, positions, updatedAt)
	if err != nil {
		return nil, err
	}
	roles := make([]*model.Role, 0, len(rows))
	for i := range rows {
		roles = append(roles, roleFromRow(&rows[i]))
	}
	return roles, nil
}

func (s *SQLStore) DeleteGuildRole(ctx context.Context, guildID, roleID, deletedAt int64) (*model.Role, error) {
	row, err := scanOne(ctx, s.q, deleteGuildRoleQuery, pgx.RowToStructByName[roleRow], guildID, roleID, deletedAt)
	if err != nil {
		return nil, err
	}
	return roleFromRow(&row), nil
}

func (s *SQLStore) AddGuildMemberRole(ctx context.Context, guildID, userID, roleID, createdAt int64) error {
	_, err := s.q.Exec(ctx, addGuildMemberRoleStatement, guildID, userID, roleID, createdAt)
	return err
}

func (s *SQLStore) RemoveGuildMemberRole(ctx context.Context, guildID, userID, roleID int64) error {
	_, err := s.q.Exec(ctx, removeGuildMemberRoleStatement, guildID, userID, roleID)
	return err
}

func (s *SQLStore) DeleteGuildMemberRoleAssignments(ctx context.Context, guildID, userID int64) error {
	_, err := s.q.Exec(ctx, deleteGuildMemberRoleAssignmentsStatement, guildID, userID)
	return err
}

func (s *SQLStore) DeleteGuildRoleAssignments(ctx context.Context, guildID, roleID int64) error {
	_, err := s.q.Exec(ctx, deleteGuildRoleAssignmentsStatement, guildID, roleID)
	return err
}

func (s *SQLStore) DeleteAllGuildRoleAssignments(ctx context.Context, guildID int64) error {
	_, err := s.q.Exec(ctx, deleteAllGuildRoleAssignmentsStatement, guildID)
	return err
}

func (s *SQLStore) ListGuildMemberRoles(ctx context.Context, guildID, userID int64) ([]*model.Role, error) {
	rows, err := scanMany(ctx, s.q, listGuildMemberRolesQuery, pgx.RowToStructByName[roleRow], guildID, userID)
	if err != nil {
		return nil, err
	}
	roles := make([]*model.Role, 0, len(rows))
	for i := range rows {
		roles = append(roles, roleFromRow(&rows[i]))
	}
	return roles, nil
}

func (s *SQLStore) ListGuildMemberRolesByGuilds(ctx context.Context, guildIDs []int64, userID int64) ([]*model.Role, error) {
	if len(guildIDs) == 0 {
		return nil, nil
	}
	rows, err := scanMany(ctx, s.q, listGuildMemberRolesByGuildsQuery, pgx.RowToStructByName[roleRow], guildIDs, userID)
	if err != nil {
		return nil, err
	}
	roles := make([]*model.Role, 0, len(rows))
	for i := range rows {
		roles = append(roles, roleFromRow(&rows[i]))
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
