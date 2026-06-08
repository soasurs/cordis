package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"

	"github.com/soasurs/cordis/pkg/outbox"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

var ErrPermissionDenied = errors.New("permission denied")

type CreateMessageParams struct {
	MessageID           int64
	ChannelID           int64
	AuthorID            int64
	Content             string
	Type                int32
	Flags               int32
	ReferencedMessageID int64
	ReferencedChannelID int64
	Attachments         []model.Attachment
}

type UpdateMessageParams struct {
	MessageID        int64
	ActorUserID      int64
	HasModPermission bool // when true, skip author_id ownership check
	Content          *string
	Flags            *int32
	Attachments      *[]model.Attachment
}

type ListMessagesParams struct {
	ChannelID int64
	Before    int64
	After     int64
	Around    int64
	Limit     int
}

type ReactionKey struct {
	MessageID int64
	EmojiID   int64
	EmojiName string
}

type Store interface {
	Transact(ctx context.Context, fn func(txStore Store) error) error
	CreateMessage(ctx context.Context, params CreateMessageParams) (*model.Message, error)
	GetMessage(ctx context.Context, messageID int64) (*model.Message, error)
	ListMessages(ctx context.Context, params ListMessagesParams) ([]*model.Message, error)
	UpdateMessage(ctx context.Context, params UpdateMessageParams) (*model.Message, error)
	DeleteMessage(ctx context.Context, messageID, actorUserID int64, hasModPermission bool) error
	ReplaceMessageMentions(ctx context.Context, messageID int64, userIDs []int64) error
	ListMentionUserIDs(ctx context.Context, messageID int64) ([]int64, error)
	AddReaction(ctx context.Context, messageID, userID, emojiID int64, emojiName string) error
	RemoveReaction(ctx context.Context, messageID, userID, emojiID int64, emojiName string) error
	ListReactionSummaries(ctx context.Context, messageIDs []int64, viewerUserID int64) (map[int64][]*model.ReactionSummary, error)
	ListReactionUsers(ctx context.Context, key ReactionKey, cursor int64, limit int) ([]int64, error)

	// InsertOutboxEvent is called inside Transact to atomically enqueue an
	// outbox event alongside business data. The relay picks it up and
	// publishes to Kafka.
	InsertOutboxEvent(ctx context.Context, evt outbox.Event) error
}

type SQLStore struct {
	db *sqlx.DB
	q  sqlx.ExtContext
}

func New(db *sqlx.DB) Store {
	return &SQLStore{
		db: db,
		q:  db,
	}
}

func (s *SQLStore) Transact(ctx context.Context, fn func(txStore Store) error) error {
	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}

	txStore := &SQLStore{
		db: s.db,
		q:  tx,
	}

	if err := fn(txStore); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

func checkRowsAffected(res sql.Result) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// InsertOutboxEvent delegates to the shared outbox package, writing the
// event within the current transaction (s.q).
func (s *SQLStore) InsertOutboxEvent(ctx context.Context, evt outbox.Event) error {
	return outbox.Insert(ctx, s.q, evt)
}
