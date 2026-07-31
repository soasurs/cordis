package store

import (
	"context"

	"github.com/jmoiron/sqlx"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

type memberRoleRow struct {
	roleRow
	UserID int64 `db:"user_id"`
}

// ListGuildMemberIDsPage returns active member IDs ordered ascending by user
// ID, continuing strictly after afterUserID.
func (s *SQLStore) ListGuildMemberIDsPage(ctx context.Context, guildID, afterUserID int64, limit int) ([]int64, error) {
	var userIDs []int64
	if err := sqlx.SelectContext(ctx, s.q, &userIDs, listGuildMemberIDsPageQuery, guildID, afterUserID, limit); err != nil {
		return nil, err
	}
	return userIDs, nil
}

// ListGuildRoleTargetIDsPage returns active members assigned to at least one
// of roleIDs, ordered ascending by user ID and continuing strictly after
// afterUserID.
func (s *SQLStore) ListGuildRoleTargetIDsPage(ctx context.Context, guildID int64, roleIDs []int64, afterUserID int64, limit int) ([]int64, error) {
	var userIDs []int64
	if err := sqlx.SelectContext(ctx, s.q, &userIDs, listGuildRoleTargetIDsPageQuery, guildID, roleIDs, afterUserID, limit); err != nil {
		return nil, err
	}
	return userIDs, nil
}

// ListGuildMemberRolesByUsers returns the effective role set for each user,
// including the implicit default role. Users without any role map to an
// empty (default-role-only) slice.
func (s *SQLStore) ListGuildMemberRolesByUsers(ctx context.Context, guildID int64, userIDs []int64) (map[int64][]*model.Role, error) {
	rolesByUser := make(map[int64][]*model.Role, len(userIDs))
	if len(userIDs) == 0 {
		return rolesByUser, nil
	}
	var rows []*memberRoleRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, listGuildMemberRolesByUsersQuery, guildID, userIDs); err != nil {
		return nil, err
	}
	for _, row := range rows {
		rolesByUser[row.UserID] = append(rolesByUser[row.UserID], roleFromRow(&row.roleRow))
	}
	return rolesByUser, nil
}
