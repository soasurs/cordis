package store

import (
	"context"
	"database/sql"

	"github.com/jackc/pgx/v5"

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
	row, err := scanOne(
		ctx, s.q, createGuildInviteQuery, pgx.RowToStructByName[guildInviteRow],
		invite.ID,
		invite.Code,
		invite.GuildID,
		invite.CreatorUserID,
		invite.MaxUses,
		invite.ExpiresAt,
		invite.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return guildInviteFromRow(&row), nil
}

func (s *SQLStore) GetGuildInvite(ctx context.Context, code string) (*model.GuildInvite, error) {
	row, err := scanOne(ctx, s.q, getGuildInviteQuery, pgx.RowToStructByName[guildInviteRow], code)
	if err != nil {
		return nil, err
	}
	return guildInviteFromRow(&row), nil
}

func (s *SQLStore) GetGuildInviteByID(ctx context.Context, inviteID int64) (*model.GuildInvite, error) {
	row, err := scanOne(ctx, s.q, getGuildInviteByIDQuery, pgx.RowToStructByName[guildInviteRow], inviteID)
	if err != nil {
		return nil, err
	}
	return guildInviteFromRow(&row), nil
}

func (s *SQLStore) ListGuildInvites(ctx context.Context, params ListGuildInvitesParams) ([]*model.GuildInvite, error) {
	rows, err := scanMany(ctx, s.q, listGuildInvitesQuery, pgx.RowToStructByName[guildInviteRow], params.GuildID, params.BeforeID, params.Limit)
	if err != nil {
		return nil, err
	}
	invites := make([]*model.GuildInvite, 0, len(rows))
	for i := range rows {
		invites = append(invites, guildInviteFromRow(&rows[i]))
	}
	return invites, nil
}

func (s *SQLStore) ConsumeGuildInvite(ctx context.Context, code string, now int64) (*model.GuildInvite, error) {
	row, err := scanOne(ctx, s.q, consumeGuildInviteQuery, pgx.RowToStructByName[guildInviteRow], code, now)
	if err != nil {
		return nil, err
	}
	return guildInviteFromRow(&row), nil
}

func (s *SQLStore) DeleteGuildInvite(ctx context.Context, code string) error {
	tag, err := s.q.Exec(ctx, deleteGuildInviteStatement, code)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLStore) DeleteGuildInvites(ctx context.Context, guildID int64) error {
	_, err := s.q.Exec(ctx, deleteGuildInvitesStatement, guildID)
	return err
}

func (s *SQLStore) GetGuild(ctx context.Context, guildID int64) (*model.Guild, error) {
	row, err := scanOne(ctx, s.q, getGuildQuery, pgx.RowToStructByName[guildRow], guildID)
	if err != nil {
		return nil, err
	}
	return guildFromRow(&row), nil
}

func (s *SQLStore) CountGuildMembers(ctx context.Context, guildID int64) (int64, error) {
	return scanOne(ctx, s.q, countGuildMembersQuery, pgx.RowTo[int64], guildID)
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
