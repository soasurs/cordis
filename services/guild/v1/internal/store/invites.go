package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

type guildInviteRow struct {
	ID            int64  `db:"id"`
	Code          string `db:"code"`
	GuildID       int64  `db:"guild_id"`
	CreatorUserID int64  `db:"creator_user_id"`
	MaxUses       int32  `db:"max_uses"`
	Uses          int32  `db:"uses"`
	ExpiresAt     int64  `db:"expires_at"`
	CreatedAt     int64  `db:"created_at"`
}

func (s *SQLStore) CreateGuildInvite(ctx context.Context, invite *model.GuildInvite) (*model.GuildInvite, error) {
	row := new(guildInviteRow)
	if err := sqlx.GetContext(
		ctx,
		s.q,
		row,
		createGuildInviteQuery,
		invite.ID,
		invite.Code,
		invite.GuildID,
		invite.CreatorUserID,
		invite.MaxUses,
		invite.ExpiresAt,
		invite.CreatedAt,
	); err != nil {
		return nil, err
	}
	return guildInviteFromRow(row), nil
}

func (s *SQLStore) GetGuildInvite(ctx context.Context, code string) (*model.GuildInvite, error) {
	row := new(guildInviteRow)
	if err := sqlx.GetContext(ctx, s.q, row, getGuildInviteQuery, code); err != nil {
		return nil, err
	}
	return guildInviteFromRow(row), nil
}

func (s *SQLStore) ListGuildInvites(ctx context.Context, params ListGuildInvitesParams) ([]*model.GuildInvite, error) {
	var rows []*guildInviteRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, listGuildInvitesQuery, params.GuildID, params.BeforeID, params.Limit); err != nil {
		return nil, err
	}
	invites := make([]*model.GuildInvite, 0, len(rows))
	for _, row := range rows {
		invites = append(invites, guildInviteFromRow(row))
	}
	return invites, nil
}

func (s *SQLStore) ConsumeGuildInvite(ctx context.Context, code string, now int64) (*model.GuildInvite, error) {
	row := new(guildInviteRow)
	if err := sqlx.GetContext(ctx, s.q, row, consumeGuildInviteQuery, code, now); err != nil {
		return nil, err
	}
	return guildInviteFromRow(row), nil
}

func (s *SQLStore) DeleteGuildInvite(ctx context.Context, code string) error {
	result, err := s.q.ExecContext(ctx, deleteGuildInviteStatement, code)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLStore) DeleteGuildInvites(ctx context.Context, guildID int64) error {
	_, err := s.q.ExecContext(ctx, deleteGuildInvitesStatement, guildID)
	return err
}

func (s *SQLStore) GetGuild(ctx context.Context, guildID int64) (*model.Guild, error) {
	row := new(guildRow)
	if err := sqlx.GetContext(ctx, s.q, row, getGuildQuery, guildID); err != nil {
		return nil, err
	}
	return guildFromRow(row), nil
}

func (s *SQLStore) CountGuildMembers(ctx context.Context, guildID int64) (int64, error) {
	var count int64
	if err := sqlx.GetContext(ctx, s.q, &count, countGuildMembersQuery, guildID); err != nil {
		return 0, err
	}
	return count, nil
}

func guildInviteFromRow(row *guildInviteRow) *model.GuildInvite {
	return &model.GuildInvite{
		ID:            row.ID,
		Code:          row.Code,
		GuildID:       row.GuildID,
		CreatorUserID: row.CreatorUserID,
		MaxUses:       row.MaxUses,
		Uses:          row.Uses,
		ExpiresAt:     row.ExpiresAt,
		CreatedAt:     row.CreatedAt,
	}
}
