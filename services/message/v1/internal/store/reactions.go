package store

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

type reactionSummaryRow struct {
	MessageID int64  `db:"message_id"`
	EmojiID   int64  `db:"emoji_id"`
	EmojiName string `db:"emoji_name"`
	Animated  bool   `db:"animated"`
	ImageKey  string `db:"image_key"`
	Count     int64  `db:"count"`
	Me        bool   `db:"me"`
}

func (s *SQLStore) AddReaction(ctx context.Context, messageID, userID, emojiID int64, emojiName string) error {
	_, err := s.q.ExecContext(ctx, AddReactionStatement, messageID, userID, emojiID, emojiName, time.Now().UnixMilli())
	return err
}

func (s *SQLStore) RemoveReaction(ctx context.Context, messageID, userID, emojiID int64, emojiName string) error {
	_, err := s.q.ExecContext(ctx, RemoveReactionStatement, messageID, userID, emojiID, emojiName)
	return err
}

func (s *SQLStore) ListReactionSummaries(ctx context.Context, messageIDs []int64, viewerUserID int64) (map[int64][]*model.ReactionSummary, error) {
	values := uniquePositiveIDs(messageIDs)
	if len(values) == 0 {
		return map[int64][]*model.ReactionSummary{}, nil
	}

	var rows []reactionSummaryRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, ListReactionSummariesQuery, pq.Array(values), viewerUserID); err != nil {
		return nil, err
	}

	summaries := make(map[int64][]*model.ReactionSummary, len(values))
	for _, row := range rows {
		summaries[row.MessageID] = append(summaries[row.MessageID], &model.ReactionSummary{
			Emoji: model.Emoji{
				ID:       row.EmojiID,
				Name:     row.EmojiName,
				Animated: row.Animated,
				ImageKey: row.ImageKey,
			},
			Count: row.Count,
			Me:    row.Me,
		})
	}
	return summaries, nil
}

func (s *SQLStore) ListReactionUsers(ctx context.Context, key ReactionKey, cursor int64, limit int) ([]int64, error) {
	if limit <= 0 {
		limit = 100
	}
	var userIDs []int64
	if err := sqlx.SelectContext(ctx, s.q, &userIDs, ListReactionUsersQuery, key.MessageID, key.EmojiID, key.EmojiName, cursor, limit); err != nil {
		return nil, err
	}
	return userIDs, nil
}
