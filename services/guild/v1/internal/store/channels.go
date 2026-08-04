package store

import (
	"context"
	"database/sql"

	"github.com/jackc/pgx/v5"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

type channelRow struct {
	ID              int64  `db:"id"`
	GuildID         int64  `db:"guild_id"`
	Name            string `db:"name"`
	Type            int32  `db:"type"`
	Position        int32  `db:"position"`
	Topic           string `db:"topic"`
	Revision        int64  `db:"revision"`
	CreatedAt       int64  `db:"created_at"`
	UpdatedAt       int64  `db:"updated_at"`
	DeletedAt       int64  `db:"deleted_at"`
	ParentID        int64  `db:"parent_id"`
	LayoutRevision  int64  `db:"channel_layout_revision"`
	HasChannel      bool   `db:"has_channel"`
	SnapshotGuildID int64  `db:"snapshot_guild_id"`
}

type channelOverwriteRow struct {
	ChannelID   int64 `db:"channel_id"`
	GuildID     int64 `db:"guild_id"`
	AppliesTo   int32 `db:"applies_to"`
	AppliesToID int64 `db:"applies_to_id"`
	Allow       int64 `db:"allow_bits"`
	Deny        int64 `db:"deny_bits"`
	Revision    int64 `db:"revision"`
	CreatedAt   int64 `db:"created_at"`
	UpdatedAt   int64 `db:"updated_at"`
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
	row, err := scanOne(ctx, s.q, createGuildChannelQuery, pgx.RowToStructByNameLax[channelRow], channelID, guildID, name, channelType, position, topic, parentID, createdAt)
	if err != nil {
		return nil, err
	}
	return channelFromRow(&row), nil
}

func (s *SQLStore) GetGuildChannel(ctx context.Context, channelID int64) (*model.Channel, error) {
	row, err := scanOne(ctx, s.q, getGuildChannelQuery, pgx.RowToStructByNameLax[channelRow], channelID)
	if err != nil {
		return nil, err
	}
	return channelFromRow(&row), nil
}

func (s *SQLStore) ListGuildChannels(ctx context.Context, guildID int64) ([]*model.Channel, error) {
	rows, err := scanMany(ctx, s.q, listGuildChannelsQuery, pgx.RowToStructByNameLax[channelRow], guildID)
	if err != nil {
		return nil, err
	}
	channels := make([]*model.Channel, 0, len(rows))
	for i := range rows {
		channels = append(channels, channelFromRow(&rows[i]))
	}
	return channels, nil
}

func (s *SQLStore) ListGuildChannelsWithRevision(
	ctx context.Context,
	guildID int64,
) ([]*model.Channel, int64, error) {
	rows, err := scanMany(ctx, s.q, listGuildChannelsWithRevisionQuery, pgx.RowToStructByNameLax[channelRow], guildID)
	if err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return nil, 0, sql.ErrNoRows
	}
	channels := make([]*model.Channel, 0, len(rows))
	for i := range rows {
		if rows[i].HasChannel {
			channels = append(channels, channelFromRow(&rows[i]))
		}
	}
	return channels, rows[0].LayoutRevision, nil
}

func (s *SQLStore) ListGuildChannelsWithRevisionsByGuilds(
	ctx context.Context,
	guildIDs []int64,
) ([]*model.Channel, map[int64]int64, error) {
	revisions := make(map[int64]int64, len(guildIDs))
	if len(guildIDs) == 0 {
		return nil, revisions, nil
	}
	rows, err := scanMany(ctx, s.q, listGuildChannelsWithRevisionsByGuildsQuery, pgx.RowToStructByNameLax[channelRow], guildIDs)
	if err != nil {
		return nil, nil, err
	}
	channels := make([]*model.Channel, 0, len(rows))
	for i := range rows {
		revisions[rows[i].SnapshotGuildID] = rows[i].LayoutRevision
		if rows[i].HasChannel {
			channels = append(channels, channelFromRow(&rows[i]))
		}
	}
	return channels, revisions, nil
}

func (s *SQLStore) ListGuildChannelsByGuilds(ctx context.Context, guildIDs []int64) ([]*model.Channel, error) {
	if len(guildIDs) == 0 {
		return nil, nil
	}
	rows, err := scanMany(ctx, s.q, listGuildChannelsByGuildsQuery, pgx.RowToStructByNameLax[channelRow], guildIDs)
	if err != nil {
		return nil, err
	}
	channels := make([]*model.Channel, 0, len(rows))
	for i := range rows {
		channels = append(channels, channelFromRow(&rows[i]))
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
	row, err := scanOne(
		ctx, s.q, updateGuildChannelQuery, pgx.RowToStructByNameLax[channelRow],
		params.ChannelID, params.Name != nil, name, params.Topic != nil, topic,
		params.ParentID != nil, parentID, params.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return channelFromRow(&row), nil
}

func (s *SQLStore) UpdateGuildChannelPosition(ctx context.Context, guildID, channelID int64, position int32, updatedAt int64) (*model.Channel, error) {
	row, err := scanOne(ctx, s.q, updateGuildChannelPositionQuery, pgx.RowToStructByNameLax[channelRow], guildID, channelID, position, updatedAt)
	if err != nil {
		return nil, err
	}
	return channelFromRow(&row), nil
}

func (s *SQLStore) UpdateGuildChannelPositions(ctx context.Context, guildID int64, updates []GuildChannelPositionUpdate, updatedAt int64) ([]*model.Channel, error) {
	if len(updates) == 0 {
		return nil, nil
	}
	channelIDs := make([]int64, 0, len(updates))
	positions := make([]int32, 0, len(updates))
	parentIDs := make([]int64, 0, len(updates))
	for _, update := range updates {
		channelIDs = append(channelIDs, update.ChannelID)
		positions = append(positions, update.Position)
		parentIDs = append(parentIDs, update.ParentID)
	}
	rows, err := scanMany(ctx, s.q, updateGuildChannelPositionsQuery, pgx.RowToStructByNameLax[channelRow],
		guildID, channelIDs, positions, parentIDs, updatedAt,
	)
	if err != nil {
		return nil, err
	}
	channels := make([]*model.Channel, 0, len(rows))
	for i := range rows {
		channels = append(channels, channelFromRow(&rows[i]))
	}
	return channels, nil
}

func (s *SQLStore) DeleteGuildChannel(ctx context.Context, channelID, deletedAt int64) (*model.Channel, error) {
	row, err := scanOne(ctx, s.q, deleteGuildChannelQuery, pgx.RowToStructByNameLax[channelRow], channelID, deletedAt)
	if err != nil {
		return nil, err
	}
	return channelFromRow(&row), nil
}

func (s *SQLStore) DeleteGuildChannels(ctx context.Context, guildID, deletedAt int64) error {
	_, err := s.q.Exec(ctx, deleteGuildChannelsStatement, guildID, deletedAt)
	return err
}

func (s *SQLStore) ClearGuildChannelParent(ctx context.Context, guildID, parentID, updatedAt int64) error {
	_, err := s.q.Exec(ctx, clearGuildChannelParentStatement, guildID, parentID, updatedAt)
	return err
}

func (s *SQLStore) UpsertGuildChannelPermissionOverwrite(
	ctx context.Context,
	overwrite *model.ChannelPermissionOverwrite,
) (*model.ChannelPermissionOverwrite, error) {
	row, err := scanOne(
		ctx, s.q, upsertGuildChannelPermissionOverwriteQuery, pgx.RowToStructByName[channelOverwriteRow],
		overwrite.ChannelID, overwrite.GuildID, overwrite.AppliesTo, overwrite.AppliesToID,
		int64(overwrite.Allow), int64(overwrite.Deny), overwrite.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return channelOverwriteFromRow(&row), nil
}

func (s *SQLStore) DeleteGuildChannelPermissionOverwrite(ctx context.Context, channelID int64, appliesTo int32, appliesToID int64) error {
	_, err := s.q.Exec(ctx, deleteGuildChannelPermissionOverwriteStatement, channelID, appliesTo, appliesToID)
	return err
}

func (s *SQLStore) DeleteGuildChannelPermissionOverwrites(ctx context.Context, channelID int64) error {
	_, err := s.q.Exec(ctx, deleteGuildChannelPermissionOverwritesStatement, channelID)
	return err
}

func (s *SQLStore) DeleteAllGuildChannelPermissionOverwrites(ctx context.Context, guildID int64) error {
	_, err := s.q.Exec(ctx, deleteAllGuildChannelPermissionOverwritesStatement, guildID)
	return err
}

func (s *SQLStore) DeleteGuildChannelPermissionOverwritesForAppliesTo(ctx context.Context, guildID int64, appliesTo int32, appliesToID int64) error {
	_, err := s.q.Exec(ctx, deleteGuildChannelPermissionOverwritesForAppliesToStatement, guildID, appliesTo, appliesToID)
	return err
}

func (s *SQLStore) ListGuildChannelPermissionOverwrites(ctx context.Context, channelID int64) ([]*model.ChannelPermissionOverwrite, error) {
	rows, err := scanMany(ctx, s.q, listGuildChannelPermissionOverwritesQuery, pgx.RowToStructByName[channelOverwriteRow], channelID)
	if err != nil {
		return nil, err
	}
	overwrites := make([]*model.ChannelPermissionOverwrite, 0, len(rows))
	for i := range rows {
		overwrites = append(overwrites, channelOverwriteFromRow(&rows[i]))
	}
	return overwrites, nil
}

func (s *SQLStore) ListGuildChannelPermissionOverwritesByChannels(ctx context.Context, channelIDs []int64) ([]*model.ChannelPermissionOverwrite, error) {
	if len(channelIDs) == 0 {
		return nil, nil
	}
	rows, err := scanMany(ctx, s.q, listGuildChannelPermissionOverwritesByChannelsQuery, pgx.RowToStructByName[channelOverwriteRow], channelIDs)
	if err != nil {
		return nil, err
	}
	overwrites := make([]*model.ChannelPermissionOverwrite, 0, len(rows))
	for i := range rows {
		overwrites = append(overwrites, channelOverwriteFromRow(&rows[i]))
	}
	return overwrites, nil
}

func (s *SQLStore) ListGuildChannelPermissionOverwritesByGuild(ctx context.Context, guildID int64) ([]*model.ChannelPermissionOverwrite, error) {
	rows, err := scanMany(ctx, s.q, listGuildChannelPermissionOverwritesByGuildQuery, pgx.RowToStructByName[channelOverwriteRow], guildID)
	if err != nil {
		return nil, err
	}
	overwrites := make([]*model.ChannelPermissionOverwrite, 0, len(rows))
	for i := range rows {
		overwrites = append(overwrites, channelOverwriteFromRow(&rows[i]))
	}
	return overwrites, nil
}

func (s *SQLStore) ListGuildChannelPermissionOverwritesByGuilds(ctx context.Context, guildIDs []int64, userID int64) ([]*model.ChannelPermissionOverwrite, error) {
	if len(guildIDs) == 0 {
		return nil, nil
	}
	rows, err := scanMany(ctx, s.q, listGuildChannelPermissionOverwritesByGuildsQuery, pgx.RowToStructByName[channelOverwriteRow], guildIDs, userID)
	if err != nil {
		return nil, err
	}
	overwrites := make([]*model.ChannelPermissionOverwrite, 0, len(rows))
	for i := range rows {
		overwrites = append(overwrites, channelOverwriteFromRow(&rows[i]))
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
		ChannelID: row.ChannelID, GuildID: row.GuildID, AppliesTo: row.AppliesTo,
		AppliesToID: row.AppliesToID, Allow: uint64(row.Allow), Deny: uint64(row.Deny),
		Revision: row.Revision, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
