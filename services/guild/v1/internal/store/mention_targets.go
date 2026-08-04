package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

type memberRoleRow struct {
	roleRow
	UserID int64 `db:"user_id"`
}

// ListGuildMemberIDsPage returns active member IDs ordered ascending by user
// ID, continuing strictly after afterUserID.
func (s *SQLStore) ListGuildMemberIDsPage(ctx context.Context, guildID, afterUserID int64, limit int) ([]int64, error) {
	return scanMany(ctx, s.q, listGuildMemberIDsPageQuery, pgx.RowTo[int64], guildID, afterUserID, limit)
}

// ListGuildRoleTargetIDsPage returns active members assigned to at least one
// of roleIDs, ordered ascending by user ID and continuing strictly after
// afterUserID.
func (s *SQLStore) ListGuildRoleTargetIDsPage(ctx context.Context, guildID int64, roleIDs []int64, afterUserID int64, limit int) ([]int64, error) {
	return scanMany(ctx, s.q, listGuildRoleTargetIDsPageQuery, pgx.RowTo[int64], guildID, roleIDs, afterUserID, limit)
}

// ListGuildMemberRolesByUsers returns the effective role set for each user,
// including the implicit default role. Users without any role map to an
// empty (default-role-only) slice.
func (s *SQLStore) ListGuildMemberRolesByUsers(ctx context.Context, guildID int64, userIDs []int64) (map[int64][]*model.Role, error) {
	rolesByUser := make(map[int64][]*model.Role, len(userIDs))
	if len(userIDs) == 0 {
		return rolesByUser, nil
	}
	rows, err := scanMany(ctx, s.q, listGuildMemberRolesByUsersQuery, pgx.RowToStructByName[memberRoleRow], guildID, userIDs)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rolesByUser[rows[i].UserID] = append(rolesByUser[rows[i].UserID], roleFromRow(&rows[i].roleRow))
	}
	return rolesByUser, nil
}
