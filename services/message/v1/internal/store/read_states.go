package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

type channelReadStateRow struct {
	UserID            int64 `db:"user_id"`
	ChannelID         int64 `db:"channel_id"`
	LastReadMessageID int64 `db:"last_read_message_id"`
	MentionCount      int32 `db:"mention_count"`
	UpdatedAt         int64 `db:"updated_at"`
}

func (s *SQLStore) AckMessage(ctx context.Context, userID, channelID, messageID int64) error {
	result, err := s.q.ExecContext(ctx, upsertChannelReadStateStatement, userID, channelID, messageID, time.Now().UnixMilli())
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

func (s *SQLStore) ListChannelReadStates(ctx context.Context, userID int64, channelIDs []int64) ([]*model.ChannelReadState, error) {
	if len(channelIDs) == 0 {
		return nil, nil
	}
	var rows []*channelReadStateRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, listChannelReadStatesQuery, userID, pq.Array(channelIDs)); err != nil {
		return nil, err
	}
	states := make([]*model.ChannelReadState, 0, len(rows))
	for _, row := range rows {
		states = append(states, &model.ChannelReadState{
			UserID:            row.UserID,
			ChannelID:         row.ChannelID,
			LastReadMessageID: row.LastReadMessageID,
			UpdatedAt:         row.UpdatedAt,
		})
	}
	return states, nil
}

func (s *SQLStore) CountMissingMessages(ctx context.Context, channelID, lastReadMessageID, userID int64) (int32, error) {
	var count int32
	if err := sqlx.GetContext(ctx, s.q, &count, countMissingMessagesQuery, channelID, lastReadMessageID, userID); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *SQLStore) CountUnreadMentions(ctx context.Context, userID, channelID, lastReadMessageID int64) (int32, error) {
	// When the user has no read state, lastReadMessageID is zero and every
	// mention row matches. The Postgres planner walks the new index on
	// (user_id, message_id) in descending order, fetching the matching
	// message row from the primary key; the scan stops when m.id drops
	// below the watermark.
	var count int32
	if err := sqlx.GetContext(ctx, s.q, &count, countUnreadMentionsQuery, userID, channelID, lastReadMessageID); err != nil {
		return 0, err
	}
	return count, nil
}
