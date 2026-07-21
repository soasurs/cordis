package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"

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
	res, err := s.q.ExecContext(
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
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLStore) GetDmChannel(ctx context.Context, channelID int64) (*model.DmChannel, error) {
	row := new(dmChannelRow)
	if err := sqlx.GetContext(ctx, s.q, row, getDmChannelQuery, channelID); err != nil {
		return nil, err
	}
	return dmChannelFromRow(row), nil
}

func (s *SQLStore) GetDmChannelByPair(ctx context.Context, userLo, userHi int64) (*model.DmChannel, error) {
	row := new(dmChannelRow)
	if err := sqlx.GetContext(ctx, s.q, row, getDmChannelByPairQuery, userLo, userHi); err != nil {
		return nil, err
	}
	return dmChannelFromRow(row), nil
}

func (s *SQLStore) ListDmChannels(ctx context.Context, params ListDmChannelsParams) ([]*model.DmChannel, error) {
	var rows []*dmChannelRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, listDmChannelsQuery, params.UserID, params.BeforeID, params.Limit); err != nil {
		return nil, err
	}
	channels := make([]*model.DmChannel, 0, len(rows))
	for _, row := range rows {
		channels = append(channels, dmChannelFromRow(row))
	}
	return channels, nil
}

func (s *SQLStore) ListAllDmChannels(ctx context.Context, userID int64) ([]*model.DmChannel, error) {
	var rows []*dmChannelRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, listAllDmChannelsQuery, userID); err != nil {
		return nil, err
	}
	channels := make([]*model.DmChannel, 0, len(rows))
	for _, row := range rows {
		channels = append(channels, dmChannelFromRow(row))
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
