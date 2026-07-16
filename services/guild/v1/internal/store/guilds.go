package store

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

type guildRow struct {
	ID        int64  `db:"id"`
	OwnerID   int64  `db:"owner_id"`
	Name      string `db:"name"`
	IconURI   string `db:"icon_uri"`
	Revision  int64  `db:"revision"`
	CreatedAt int64  `db:"created_at"`
	UpdatedAt int64  `db:"updated_at"`
	DeletedAt int64  `db:"deleted_at"`
}

func (s *SQLStore) CreateGuild(ctx context.Context, guildID, ownerID int64, name, iconURI string, createdAt int64) (*model.Guild, error) {
	row := new(guildRow)
	if err := sqlx.GetContext(ctx, s.q, row, createGuildQuery, guildID, ownerID, name, iconURI, createdAt); err != nil {
		return nil, err
	}
	return guildFromRow(row), nil
}

func (s *SQLStore) CreateGuildMember(ctx context.Context, guildID, userID, joinedAt int64) error {
	_, err := s.q.ExecContext(ctx, createGuildMemberStatement, guildID, userID, joinedAt)
	return err
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
		ID:        row.ID,
		OwnerID:   row.OwnerID,
		Name:      row.Name,
		IconURI:   row.IconURI,
		Revision:  row.Revision,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		DeletedAt: row.DeletedAt,
	}
}
