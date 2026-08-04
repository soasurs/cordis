package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5"

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
	row, err := scanOne(ctx, s.q, ackMessageQuery, pgx.RowToStructByName[ackMessageRow], userID, channelID, messageID, time.Now().UnixMilli())
	if err != nil {
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
	rows, err := scanMany(ctx, s.q, listReadyChannelReadStatesQuery, pgx.RowToStructByName[channelReadStateRow], userID, channelIDs)
	if err != nil {
		return nil, err
	}
	states := make([]*model.ChannelReadState, 0, len(rows))
	for i := range rows {
		row := &rows[i]
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
	messageID, err := scanOne(ctx, s.q, getLastMessageIDQuery, pgx.RowTo[int64], channelID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return messageID, nil
}
