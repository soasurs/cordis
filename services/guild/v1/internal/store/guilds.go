package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

type guildRow struct {
	ID             int64  `db:"id"`
	OwnerID        int64  `db:"owner_id"`
	Name           string `db:"name"`
	IconURI        string `db:"icon_uri"`
	Revision       int64  `db:"revision"`
	AccessRevision int64  `db:"access_revision"`
	CreatedAt      int64  `db:"created_at"`
	UpdatedAt      int64  `db:"updated_at"`
	DeletedAt      int64  `db:"deleted_at"`
}

func (s *SQLStore) CreateGuild(ctx context.Context, guildID, ownerID int64, name, iconURI string, createdAt int64) (*model.Guild, error) {
	row := new(guildRow)
	if err := sqlx.GetContext(ctx, s.q, row, createGuildQuery, guildID, ownerID, name, iconURI, createdAt); err != nil {
		return nil, err
	}
	return guildFromRow(row), nil
}

func (s *SQLStore) CreateGuildMember(ctx context.Context, guildID, userID, joinedAt int64) (*model.GuildMember, error) {
	row := new(guildMemberRow)
	err := sqlx.GetContext(ctx, s.q, row, createGuildMemberQuery, guildID, userID, joinedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMemberAlreadyExists
	}
	if err != nil {
		return nil, err
	}
	return guildMemberFromRow(row), nil
}

type guildBanRow struct {
	GuildID     int64  `db:"guild_id"`
	UserID      int64  `db:"user_id"`
	ActorUserID int64  `db:"actor_user_id"`
	Reason      string `db:"reason"`
	CreatedAt   int64  `db:"created_at"`
}

func (s *SQLStore) UpsertGuildBan(ctx context.Context, ban *model.GuildBan) (*model.GuildBan, error) {
	row := new(guildBanRow)
	if err := sqlx.GetContext(ctx, s.q, row, upsertGuildBanQuery, ban.GuildID, ban.UserID, ban.ActorUserID, ban.Reason, ban.CreatedAt); err != nil {
		return nil, err
	}
	return guildBanFromRow(row), nil
}

func (s *SQLStore) DeleteGuildBan(ctx context.Context, guildID, userID int64) error {
	result, err := s.q.ExecContext(ctx, deleteGuildBanStatement, guildID, userID)
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

func (s *SQLStore) GetGuildBan(ctx context.Context, guildID, userID int64) (*model.GuildBan, error) {
	row := new(guildBanRow)
	if err := sqlx.GetContext(ctx, s.q, row, getGuildBanQuery, guildID, userID); err != nil {
		return nil, err
	}
	return guildBanFromRow(row), nil
}

func (s *SQLStore) ListGuildBans(ctx context.Context, params ListGuildBansParams) ([]*model.GuildBan, error) {
	var rows []*guildBanRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, listGuildBansQuery, params.GuildID, params.BeforeUserID, params.Limit); err != nil {
		return nil, err
	}
	bans := make([]*model.GuildBan, 0, len(rows))
	for _, row := range rows {
		bans = append(bans, guildBanFromRow(row))
	}
	return bans, nil
}

func (s *SQLStore) DeleteGuildBans(ctx context.Context, guildID int64) error {
	_, err := s.q.ExecContext(ctx, deleteGuildBansStatement, guildID)
	return err
}

func guildBanFromRow(row *guildBanRow) *model.GuildBan {
	return &model.GuildBan{
		GuildID: row.GuildID, UserID: row.UserID, ActorUserID: row.ActorUserID,
		Reason: row.Reason, CreatedAt: row.CreatedAt,
	}
}

func (s *SQLStore) GetGuildMember(ctx context.Context, guildID, userID int64) (*model.GuildMember, error) {
	row := new(guildMemberRow)
	if err := sqlx.GetContext(ctx, s.q, row, getGuildMemberQuery, guildID, userID); err != nil {
		return nil, err
	}
	return guildMemberFromRow(row), nil
}

func (s *SQLStore) ListGuildMembers(ctx context.Context, params ListGuildMembersParams) ([]*model.GuildMember, error) {
	var rows []*guildMemberRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, listGuildMembersQuery, params.GuildID, params.BeforeUserID, params.Limit); err != nil {
		return nil, err
	}
	members := make([]*model.GuildMember, 0, len(rows))
	for _, row := range rows {
		members = append(members, guildMemberFromRow(row))
	}
	return members, nil
}

func (s *SQLStore) UpdateGuildMemberNickname(ctx context.Context, guildID, userID int64, nickname string) (*model.GuildMember, error) {
	row := new(guildMemberRow)
	if err := sqlx.GetContext(
		ctx,
		s.q,
		row,
		updateGuildMemberNicknameQuery,
		guildID,
		userID,
		nickname,
		time.Now().UnixMilli(),
	); err != nil {
		return nil, err
	}
	return guildMemberFromRow(row), nil
}

func (s *SQLStore) RemoveGuildMember(ctx context.Context, guildID, userID, removedAt int64) (*model.GuildMember, error) {
	row := new(guildMemberRow)
	if err := sqlx.GetContext(ctx, s.q, row, removeGuildMemberQuery, guildID, userID, removedAt); err != nil {
		return nil, err
	}
	return guildMemberFromRow(row), nil
}

func (s *SQLStore) TransferGuildOwnership(ctx context.Context, guildID, currentOwnerID, newOwnerID int64) (*model.Guild, error) {
	row := new(guildRow)
	if err := sqlx.GetContext(
		ctx,
		s.q,
		row,
		transferGuildOwnershipQuery,
		guildID,
		currentOwnerID,
		newOwnerID,
		time.Now().UnixMilli(),
	); err != nil {
		return nil, err
	}
	return guildFromRow(row), nil
}

type guildMemberRow struct {
	GuildID   int64  `db:"guild_id"`
	UserID    int64  `db:"user_id"`
	Nickname  string `db:"nickname"`
	Revision  int64  `db:"revision"`
	JoinedAt  int64  `db:"joined_at"`
	UpdatedAt int64  `db:"updated_at"`
	DeletedAt int64  `db:"deleted_at"`
}

func guildMemberFromRow(row *guildMemberRow) *model.GuildMember {
	return &model.GuildMember{
		GuildID:   row.GuildID,
		UserID:    row.UserID,
		Nickname:  row.Nickname,
		Revision:  row.Revision,
		JoinedAt:  row.JoinedAt,
		UpdatedAt: row.UpdatedAt,
		DeletedAt: row.DeletedAt,
	}
}

func (s *SQLStore) CreateDefaultRole(ctx context.Context, guildID, createdAt int64) error {
	_, err := s.q.ExecContext(ctx, createDefaultRoleStatement, guildID, createdAt)
	return err
}

func (s *SQLStore) GetGuildForMember(ctx context.Context, guildID, userID int64) (*model.Guild, error) {
	row := new(guildRow)
	if err := sqlx.GetContext(ctx, s.q, row, getGuildForMemberQuery, guildID, userID); err != nil {
		return nil, err
	}
	return guildFromRow(row), nil
}

func (s *SQLStore) ListGuildsForMemberByIDs(ctx context.Context, guildIDs []int64, userID int64) ([]*model.Guild, error) {
	if len(guildIDs) == 0 {
		return nil, nil
	}
	var rows []*guildRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, listGuildsForMemberByIDsQuery, pq.Array(guildIDs), userID); err != nil {
		return nil, err
	}
	guilds := make([]*model.Guild, 0, len(rows))
	for _, row := range rows {
		guilds = append(guilds, guildFromRow(row))
	}
	return guilds, nil
}

func (s *SQLStore) ListUserGuilds(ctx context.Context, params ListUserGuildsParams) ([]*model.Guild, error) {
	var rows []*guildRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, listUserGuildsQuery, params.UserID, params.Before, params.Limit); err != nil {
		return nil, err
	}
	guilds := make([]*model.Guild, 0, len(rows))
	for _, row := range rows {
		guilds = append(guilds, guildFromRow(row))
	}
	return guilds, nil
}

func (s *SQLStore) UpdateGuild(ctx context.Context, params UpdateGuildParams) (*model.Guild, error) {
	row := new(guildRow)
	var name, iconURI string
	if params.Name != nil {
		name = *params.Name
	}
	if params.IconURI != nil {
		iconURI = *params.IconURI
	}
	err := sqlx.GetContext(
		ctx,
		s.q,
		row,
		updateGuildQuery,
		params.GuildID,
		params.Name != nil,
		name,
		params.IconURI != nil,
		iconURI,
		time.Now().UnixMilli(),
	)
	if err != nil {
		return nil, err
	}
	return guildFromRow(row), nil
}

func (s *SQLStore) DeleteGuild(ctx context.Context, guildID, deletedAt int64) (*model.Guild, error) {
	row := new(guildRow)
	if err := sqlx.GetContext(ctx, s.q, row, deleteGuildQuery, guildID, deletedAt); err != nil {
		return nil, err
	}
	return guildFromRow(row), nil
}

func (s *SQLStore) DeleteGuildMembers(ctx context.Context, guildID, deletedAt int64) error {
	_, err := s.q.ExecContext(ctx, deleteGuildMembersStatement, guildID, deletedAt)
	return err
}

func (s *SQLStore) DeleteGuildRoles(ctx context.Context, guildID, deletedAt int64) error {
	_, err := s.q.ExecContext(ctx, deleteGuildRolesStatement, guildID, deletedAt)
	return err
}

func guildFromRow(row *guildRow) *model.Guild {
	return &model.Guild{
		ID:             row.ID,
		OwnerID:        row.OwnerID,
		Name:           row.Name,
		IconURI:        row.IconURI,
		Revision:       row.Revision,
		AccessRevision: row.AccessRevision,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
		DeletedAt:      row.DeletedAt,
	}
}
