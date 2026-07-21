package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

type ackMessageRow struct {
	TargetExists bool `db:"target_exists"`
	Advanced     bool `db:"advanced"`
}

type channelReadStateRow struct {
	UserID            int64 `db:"user_id"`
	ChannelID         int64 `db:"channel_id"`
	LastMessageID     int64 `db:"last_message_id"`
	LastReadMessageID int64 `db:"last_read_message_id"`
	MentionCount      int32 `db:"mention_count"`
	UpdatedAt         int64 `db:"updated_at"`
}

func (s *SQLStore) AckMessage(ctx context.Context, userID, channelID, messageID int64) (bool, error) {
	row := new(ackMessageRow)
	if err := sqlx.GetContext(ctx, s.q, row, ackMessageQuery, userID, channelID, messageID, time.Now().UnixMilli()); err != nil {
		return false, err
	}
	if !row.TargetExists {
		return false, sql.ErrNoRows
	}
	return row.Advanced, nil
}

func (s *SQLStore) ListReadyChannelReadStates(ctx context.Context, userID int64, channelIDs []int64) ([]*model.ChannelReadState, error) {
	if len(channelIDs) == 0 {
		return nil, nil
	}
	var rows []*channelReadStateRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, listReadyChannelReadStatesQuery, userID, pq.Array(channelIDs)); err != nil {
		return nil, err
	}
	states := make([]*model.ChannelReadState, 0, len(rows))
	for _, row := range rows {
		states = append(states, &model.ChannelReadState{
			UserID:            row.UserID,
			ChannelID:         row.ChannelID,
			LastMessageID:     row.LastMessageID,
			LastReadMessageID: row.LastReadMessageID,
			MentionCount:      row.MentionCount,
			UpdatedAt:         row.UpdatedAt,
		})
	}
	return states, nil
}

func (s *SQLStore) GetLastMessageID(ctx context.Context, channelID int64) (int64, error) {
	var messageID int64
	if err := sqlx.GetContext(ctx, s.q, &messageID, getLastMessageIDQuery, channelID); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return messageID, nil
}
