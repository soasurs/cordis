package store

import (
	"context"
	"database/sql"

	"github.com/jackc/pgx/v5"

	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

type dmChannelRow struct {
	ID        int64 `db:"id"`
	UserLo    int64 `db:"user_lo"`
	UserHi    int64 `db:"user_hi"`
	CreatedAt int64 `db:"created_at"`
}

// CreateDmChannel inserts the channel unless the pair already has one. It
// reports sql.ErrNoRows when the pair lost the race so callers can reload
// the existing channel.
func (s *SQLStore) CreateDmChannel(ctx context.Context, channel *model.DmChannel) error {
	tag, err := s.q.Exec(
		ctx,
		createDmChannelStatement,
		channel.ID,
		channel.UserLo,
		channel.UserHi,
		channel.CreatedAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLStore) GetDmChannel(ctx context.Context, channelID int64) (*model.DmChannel, error) {
	row, err := scanOne(ctx, s.q, getDmChannelQuery, pgx.RowToStructByName[dmChannelRow], channelID)
	if err != nil {
		return nil, err
	}
	return dmChannelFromRow(&row), nil
}

func (s *SQLStore) GetDmChannelByPair(ctx context.Context, userLo, userHi int64) (*model.DmChannel, error) {
	row, err := scanOne(ctx, s.q, getDmChannelByPairQuery, pgx.RowToStructByName[dmChannelRow], userLo, userHi)
	if err != nil {
		return nil, err
	}
	return dmChannelFromRow(&row), nil
}

func (s *SQLStore) ListDmChannels(ctx context.Context, params ListDmChannelsParams) ([]*model.DmChannel, error) {
	rows, err := scanMany(ctx, s.q, listDmChannelsQuery, pgx.RowToStructByName[dmChannelRow], params.UserID, params.BeforeID, params.Limit)
	if err != nil {
		return nil, err
	}
	channels := make([]*model.DmChannel, 0, len(rows))
	for i := range rows {
		channels = append(channels, dmChannelFromRow(&rows[i]))
	}
	return channels, nil
}

func (s *SQLStore) ListAllDmChannels(ctx context.Context, userID int64) ([]*model.DmChannel, error) {
	rows, err := scanMany(ctx, s.q, listAllDmChannelsQuery, pgx.RowToStructByName[dmChannelRow], userID)
	if err != nil {
		return nil, err
	}
	channels := make([]*model.DmChannel, 0, len(rows))
	for i := range rows {
		channels = append(channels, dmChannelFromRow(&rows[i]))
	}
	return channels, nil
}

func dmChannelFromRow(row *dmChannelRow) *model.DmChannel {
	return &model.DmChannel{
		ID:        row.ID,
		UserLo:    row.UserLo,
		UserHi:    row.UserHi,
		CreatedAt: row.CreatedAt,
	}
}
