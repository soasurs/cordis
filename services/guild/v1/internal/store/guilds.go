package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

type guildRow struct {
	ID                    int64  `db:"id"`
	OwnerID               int64  `db:"owner_id"`
	Name                  string `db:"name"`
	Description           string `db:"description"`
	IconAssetID           int64  `db:"icon_asset_id"`
	Revision              int64  `db:"revision"`
	AccessRevision        int64  `db:"access_revision"`
	ChannelLayoutRevision int64  `db:"channel_layout_revision"`
	CreatedAt             int64  `db:"created_at"`
	UpdatedAt             int64  `db:"updated_at"`
	DeletedAt             int64  `db:"deleted_at"`
}

func (s *SQLStore) CreateGuild(ctx context.Context, guildID, ownerID int64, name string, createdAt int64) (*model.Guild, error) {
	row, err := scanOne(ctx, s.q, createGuildQuery, pgx.RowToStructByName[guildRow], guildID, ownerID, name, createdAt)
	if err != nil {
		return nil, err
	}
	return guildFromRow(&row), nil
}

func (s *SQLStore) CreateGuildMember(ctx context.Context, guildID, userID, joinedAt int64) (*model.GuildMember, error) {
	row, err := scanOne(ctx, s.q, createGuildMemberQuery, pgx.RowToStructByName[guildMemberRow], guildID, userID, joinedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMemberAlreadyExists
	}
	if err != nil {
		return nil, err
	}
	return guildMemberFromRow(&row), nil
}

type guildBanRow struct {
	GuildID     int64  `db:"guild_id"`
	UserID      int64  `db:"user_id"`
	ActorUserID int64  `db:"actor_user_id"`
	Reason      string `db:"reason"`
	CreatedAt   int64  `db:"created_at"`
}

func (s *SQLStore) UpsertGuildBan(ctx context.Context, ban *model.GuildBan) (*model.GuildBan, error) {
	row, err := scanOne(ctx, s.q, upsertGuildBanQuery, pgx.RowToStructByName[guildBanRow], ban.GuildID, ban.UserID, ban.ActorUserID, ban.Reason, ban.CreatedAt)
	if err != nil {
		return nil, err
	}
	return guildBanFromRow(&row), nil
}

func (s *SQLStore) DeleteGuildBan(ctx context.Context, guildID, userID int64) error {
	tag, err := s.q.Exec(ctx, deleteGuildBanStatement, guildID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLStore) GetGuildBan(ctx context.Context, guildID, userID int64) (*model.GuildBan, error) {
	row, err := scanOne(ctx, s.q, getGuildBanQuery, pgx.RowToStructByName[guildBanRow], guildID, userID)
	if err != nil {
		return nil, err
	}
	return guildBanFromRow(&row), nil
}

func (s *SQLStore) ListGuildBans(ctx context.Context, params ListGuildBansParams) ([]*model.GuildBan, error) {
	rows, err := scanMany(
		ctx, s.q, listGuildBansQuery, pgx.RowToStructByName[guildBanRow],
		params.GuildID,
		params.BeforeCreatedAt,
		params.BeforeUserID,
		params.Limit,
	)
	if err != nil {
		return nil, err
	}
	bans := make([]*model.GuildBan, 0, len(rows))
	for i := range rows {
		bans = append(bans, guildBanFromRow(&rows[i]))
	}
	return bans, nil
}

func (s *SQLStore) DeleteGuildBans(ctx context.Context, guildID int64) error {
	_, err := s.q.Exec(ctx, deleteGuildBansStatement, guildID)
	return err
}

func guildBanFromRow(row *guildBanRow) *model.GuildBan {
	return &model.GuildBan{
		GuildID: row.GuildID, UserID: row.UserID, ActorUserID: row.ActorUserID,
		Reason: row.Reason, CreatedAt: row.CreatedAt,
	}
}

func (s *SQLStore) GetGuildMember(ctx context.Context, guildID, userID int64) (*model.GuildMember, error) {
	row, err := scanOne(ctx, s.q, getGuildMemberQuery, pgx.RowToStructByName[guildMemberRow], guildID, userID)
	if err != nil {
		return nil, err
	}
	return guildMemberFromRow(&row), nil
}

func (s *SQLStore) ListGuildMembers(ctx context.Context, params ListGuildMembersParams) ([]*model.GuildMember, error) {
	rows, err := scanMany(
		ctx, s.q, listGuildMembersQuery, pgx.RowToStructByName[guildMemberRow],
		params.GuildID,
		params.BeforeJoinedAt,
		params.BeforeUserID,
		params.Limit,
	)
	if err != nil {
		return nil, err
	}
	members := make([]*model.GuildMember, 0, len(rows))
	for i := range rows {
		members = append(members, guildMemberFromRow(&rows[i]))
	}
	return members, nil
}

func (s *SQLStore) ListUsersWithCommonGuild(ctx context.Context, userID int64, targetUserIDs []int64) ([]int64, error) {
	return scanMany(ctx, s.q, listUsersWithCommonGuildQuery, pgx.RowTo[int64], userID, targetUserIDs)
}

func (s *SQLStore) ListGuildRoleMembers(ctx context.Context, params ListGuildRoleMembersParams) ([]*model.GuildMember, error) {
	rows, err := scanMany(
		ctx, s.q, listGuildRoleMembersQuery, pgx.RowToStructByName[guildMemberRow],
		params.GuildID,
		params.RoleID,
		params.BeforeJoinedAt,
		params.BeforeUserID,
		params.Limit,
	)
	if err != nil {
		return nil, err
	}
	members := make([]*model.GuildMember, 0, len(rows))
	for i := range rows {
		members = append(members, guildMemberFromRow(&rows[i]))
	}
	return members, nil
}

func (s *SQLStore) UpdateGuildMemberNickname(ctx context.Context, guildID, userID int64, nickname string) (*model.GuildMember, error) {
	row, err := scanOne(
		ctx, s.q, updateGuildMemberNicknameQuery, pgx.RowToStructByName[guildMemberRow],
		guildID,
		userID,
		nickname,
		time.Now().UnixMilli(),
	)
	if err != nil {
		return nil, err
	}
	return guildMemberFromRow(&row), nil
}

func (s *SQLStore) RemoveGuildMember(ctx context.Context, guildID, userID, removedAt int64) (*model.GuildMember, error) {
	row, err := scanOne(ctx, s.q, removeGuildMemberQuery, pgx.RowToStructByName[guildMemberRow], guildID, userID, removedAt)
	if err != nil {
		return nil, err
	}
	return guildMemberFromRow(&row), nil
}

func (s *SQLStore) TransferGuildOwnership(ctx context.Context, guildID, currentOwnerID, newOwnerID int64) (*model.Guild, error) {
	row, err := scanOne(
		ctx, s.q, transferGuildOwnershipQuery, pgx.RowToStructByName[guildRow],
		guildID,
		currentOwnerID,
		newOwnerID,
		time.Now().UnixMilli(),
	)
	if err != nil {
		return nil, err
	}
	return guildFromRow(&row), nil
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
	_, err := s.q.Exec(ctx, createDefaultRoleStatement, guildID, createdAt)
	return err
}

func (s *SQLStore) GetGuildForMember(ctx context.Context, guildID, userID int64) (*model.Guild, error) {
	row, err := scanOne(ctx, s.q, getGuildForMemberQuery, pgx.RowToStructByName[guildRow], guildID, userID)
	if err != nil {
		return nil, err
	}
	return guildFromRow(&row), nil
}

func (s *SQLStore) ListUserGuilds(ctx context.Context, params ListUserGuildsParams) ([]*model.Guild, error) {
	rows, err := scanMany(ctx, s.q, listUserGuildsQuery, pgx.RowToStructByName[guildRow], params.UserID, params.Before, params.Limit)
	if err != nil {
		return nil, err
	}
	guilds := make([]*model.Guild, 0, len(rows))
	for i := range rows {
		guilds = append(guilds, guildFromRow(&rows[i]))
	}
	return guilds, nil
}

func (s *SQLStore) UpdateGuild(ctx context.Context, params UpdateGuildParams) (*model.Guild, error) {
	var name, description string
	if params.Name != nil {
		name = *params.Name
	}
	if params.Description != nil {
		description = *params.Description
	}
	row, err := scanOne(
		ctx, s.q, updateGuildQuery, pgx.RowToStructByName[guildRow],
		params.GuildID,
		params.Name != nil,
		name,
		params.Description != nil,
		description,
		time.Now().UnixMilli(),
	)
	if err != nil {
		return nil, err
	}
	return guildFromRow(&row), nil
}

func (s *SQLStore) UpdateGuildIcon(ctx context.Context, guildID, assetID int64) (*model.Guild, error) {
	row, err := scanOne(
		ctx, s.q, updateGuildIconQuery, pgx.RowToStructByName[guildRow],
		guildID,
		assetID,
		time.Now().UnixMilli(),
	)
	if err != nil {
		return nil, err
	}
	return guildFromRow(&row), nil
}

func (s *SQLStore) DeleteGuild(ctx context.Context, guildID, deletedAt int64) (*model.Guild, error) {
	row, err := scanOne(ctx, s.q, deleteGuildQuery, pgx.RowToStructByName[guildRow], guildID, deletedAt)
	if err != nil {
		return nil, err
	}
	return guildFromRow(&row), nil
}

func (s *SQLStore) DeleteGuildMembers(ctx context.Context, guildID, deletedAt int64) error {
	_, err := s.q.Exec(ctx, deleteGuildMembersStatement, guildID, deletedAt)
	return err
}

func (s *SQLStore) DeleteGuildRoles(ctx context.Context, guildID, deletedAt int64) error {
	_, err := s.q.Exec(ctx, deleteGuildRolesStatement, guildID, deletedAt)
	return err
}

func (s *SQLStore) GetGuildChannelLayoutRevision(ctx context.Context, guildID int64) (int64, error) {
	return scanOne(ctx, s.q, getGuildChannelLayoutRevisionQuery, pgx.RowTo[int64], guildID)
}

func (s *SQLStore) AdvanceGuildChannelLayoutRevision(
	ctx context.Context,
	guildID, expectedRevision int64,
) (int64, error) {
	revision, err := scanOne(
		ctx, s.q, advanceGuildChannelLayoutRevisionQuery, pgx.RowTo[int64],
		guildID,
		expectedRevision,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrGuildChannelLayoutRevisionConflict
		}
		return 0, err
	}
	return revision, nil
}

func guildFromRow(row *guildRow) *model.Guild {
	return &model.Guild{
		ID:                    row.ID,
		OwnerID:               row.OwnerID,
		Name:                  row.Name,
		Description:           row.Description,
		IconAssetID:           row.IconAssetID,
		Revision:              row.Revision,
		AccessRevision:        row.AccessRevision,
		ChannelLayoutRevision: row.ChannelLayoutRevision,
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
		DeletedAt:             row.DeletedAt,
	}
}
