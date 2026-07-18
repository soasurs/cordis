package store

import (
	"context"

	"github.com/jmoiron/sqlx"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

type channelRow struct {
	ID        int64  `db:"id"`
	GuildID   int64  `db:"guild_id"`
	Name      string `db:"name"`
	Type      int32  `db:"type"`
	Position  int32  `db:"position"`
	Topic     string `db:"topic"`
	Revision  int64  `db:"revision"`
	CreatedAt int64  `db:"created_at"`
	UpdatedAt int64  `db:"updated_at"`
	DeletedAt int64  `db:"deleted_at"`
	ParentID  int64  `db:"parent_id"`
}

type channelOverwriteRow struct {
	ChannelID  int64 `db:"channel_id"`
	GuildID    int64 `db:"guild_id"`
	TargetType int32 `db:"target_type"`
	TargetID   int64 `db:"target_id"`
	Allow      int64 `db:"allow_bits"`
	Deny       int64 `db:"deny_bits"`
	Revision   int64 `db:"revision"`
	CreatedAt  int64 `db:"created_at"`
	UpdatedAt  int64 `db:"updated_at"`
}

func (s *SQLStore) CreateGuildChannel(
	ctx context.Context,
	channelID, guildID int64,
	name string,
	channelType, position int32,
	topic string,
	parentID int64,
	createdAt int64,
) (*model.Channel, error) {
	row := new(channelRow)
	if err := sqlx.GetContext(ctx, s.q, row, createGuildChannelQuery, channelID, guildID, name, channelType, position, topic, parentID, createdAt); err != nil {
		return nil, err
	}
	return channelFromRow(row), nil
}

func (s *SQLStore) GetGuildChannel(ctx context.Context, channelID int64) (*model.Channel, error) {
	row := new(channelRow)
	if err := sqlx.GetContext(ctx, s.q, row, getGuildChannelQuery, channelID); err != nil {
		return nil, err
	}
	return channelFromRow(row), nil
}

func (s *SQLStore) ListGuildChannels(ctx context.Context, guildID int64) ([]*model.Channel, error) {
	var rows []*channelRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, listGuildChannelsQuery, guildID); err != nil {
		return nil, err
	}
	channels := make([]*model.Channel, 0, len(rows))
	for _, row := range rows {
		channels = append(channels, channelFromRow(row))
	}
	return channels, nil
}

func (s *SQLStore) UpdateGuildChannel(ctx context.Context, params UpdateGuildChannelParams) (*model.Channel, error) {
	var name, topic string
	var parentID int64
	if params.Name != nil {
		name = *params.Name
	}
	if params.Topic != nil {
		topic = *params.Topic
	}
	if params.ParentID != nil {
		parentID = *params.ParentID
	}
	row := new(channelRow)
	if err := sqlx.GetContext(
		ctx, s.q, row, updateGuildChannelQuery,
		params.ChannelID, params.Name != nil, name, params.Topic != nil, topic,
		params.ParentID != nil, parentID, params.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return channelFromRow(row), nil
}

func (s *SQLStore) UpdateGuildChannelPosition(ctx context.Context, channelID int64, position int32, updatedAt int64) (*model.Channel, error) {
	row := new(channelRow)
	if err := sqlx.GetContext(ctx, s.q, row, updateGuildChannelPositionQuery, channelID, position, updatedAt); err != nil {
		return nil, err
	}
	return channelFromRow(row), nil
}

func (s *SQLStore) DeleteGuildChannel(ctx context.Context, channelID, deletedAt int64) (*model.Channel, error) {
	row := new(channelRow)
	if err := sqlx.GetContext(ctx, s.q, row, deleteGuildChannelQuery, channelID, deletedAt); err != nil {
		return nil, err
	}
	return channelFromRow(row), nil
}

func (s *SQLStore) DeleteGuildChannels(ctx context.Context, guildID, deletedAt int64) error {
	_, err := s.q.ExecContext(ctx, deleteGuildChannelsStatement, guildID, deletedAt)
	return err
}

func (s *SQLStore) ClearGuildChannelParent(ctx context.Context, guildID, parentID, updatedAt int64) error {
	_, err := s.q.ExecContext(ctx, clearGuildChannelParentStatement, guildID, parentID, updatedAt)
	return err
}

func (s *SQLStore) UpsertGuildChannelPermissionOverwrite(
	ctx context.Context,
	overwrite *model.ChannelPermissionOverwrite,
) (*model.ChannelPermissionOverwrite, error) {
	row := new(channelOverwriteRow)
	if err := sqlx.GetContext(
		ctx, s.q, row, upsertGuildChannelPermissionOverwriteQuery,
		overwrite.ChannelID, overwrite.GuildID, overwrite.TargetType, overwrite.TargetID,
		int64(overwrite.Allow), int64(overwrite.Deny), overwrite.CreatedAt,
	); err != nil {
		return nil, err
	}
	return channelOverwriteFromRow(row), nil
}

func (s *SQLStore) DeleteGuildChannelPermissionOverwrite(ctx context.Context, channelID int64, targetType int32, targetID int64) error {
	_, err := s.q.ExecContext(ctx, deleteGuildChannelPermissionOverwriteStatement, channelID, targetType, targetID)
	return err
}

func (s *SQLStore) DeleteGuildChannelPermissionOverwrites(ctx context.Context, channelID int64) error {
	_, err := s.q.ExecContext(ctx, deleteGuildChannelPermissionOverwritesStatement, channelID)
	return err
}

func (s *SQLStore) DeleteAllGuildChannelPermissionOverwrites(ctx context.Context, guildID int64) error {
	_, err := s.q.ExecContext(ctx, deleteAllGuildChannelPermissionOverwritesStatement, guildID)
	return err
}

func (s *SQLStore) DeleteGuildChannelPermissionOverwritesForTarget(ctx context.Context, guildID int64, targetType int32, targetID int64) error {
	_, err := s.q.ExecContext(ctx, deleteGuildChannelPermissionOverwritesForTargetStatement, guildID, targetType, targetID)
	return err
}

func (s *SQLStore) ListGuildChannelPermissionOverwrites(ctx context.Context, channelID int64) ([]*model.ChannelPermissionOverwrite, error) {
	var rows []*channelOverwriteRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, listGuildChannelPermissionOverwritesQuery, channelID); err != nil {
		return nil, err
	}
	overwrites := make([]*model.ChannelPermissionOverwrite, 0, len(rows))
	for _, row := range rows {
		overwrites = append(overwrites, channelOverwriteFromRow(row))
	}
	return overwrites, nil
}

func (s *SQLStore) ListGuildChannelPermissionOverwritesByGuild(ctx context.Context, guildID int64) ([]*model.ChannelPermissionOverwrite, error) {
	var rows []*channelOverwriteRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, listGuildChannelPermissionOverwritesByGuildQuery, guildID); err != nil {
		return nil, err
	}
	overwrites := make([]*model.ChannelPermissionOverwrite, 0, len(rows))
	for _, row := range rows {
		overwrites = append(overwrites, channelOverwriteFromRow(row))
	}
	return overwrites, nil
}

func channelFromRow(row *channelRow) *model.Channel {
	return &model.Channel{
		ID: row.ID, GuildID: row.GuildID, Name: row.Name, Type: row.Type,
		Position: row.Position, Topic: row.Topic, Revision: row.Revision,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, DeletedAt: row.DeletedAt,
		ParentID: row.ParentID,
	}
}

func channelOverwriteFromRow(row *channelOverwriteRow) *model.ChannelPermissionOverwrite {
	return &model.ChannelPermissionOverwrite{
		ChannelID: row.ChannelID, GuildID: row.GuildID, TargetType: row.TargetType,
		TargetID: row.TargetID, Allow: uint64(row.Allow), Deny: uint64(row.Deny),
		Revision: row.Revision, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
