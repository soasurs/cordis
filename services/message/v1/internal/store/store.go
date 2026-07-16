package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"

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
	HasModPermission bool
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

type Store interface {
	Transact(ctx context.Context, fn func(txStore Store) error) error
	CreateMessage(ctx context.Context, params CreateMessageParams) (*model.Message, error)
	GetMessage(ctx context.Context, messageID int64) (*model.Message, error)
	ListMessages(ctx context.Context, params ListMessagesParams) ([]*model.Message, error)
	UpdateMessage(ctx context.Context, params UpdateMessageParams) (*model.Message, error)
	DeleteMessage(ctx context.Context, messageID, actorUserID int64, hasModPermission bool) (*model.Message, error)
	ReplaceMessageMentions(ctx context.Context, messageID int64, userIDs []int64) error
	ListMentionUserIDs(ctx context.Context, messageID int64) ([]int64, error)
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

func (s *SQLStore) Transact(ctx context.Context, fn func(txStore Store) error) (err error) {
	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{})
	if err != nil {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	err = fn(&SQLStore{db: s.db, q: tx})
	if err != nil {
		return
	}
	err = tx.Commit()
	return
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
