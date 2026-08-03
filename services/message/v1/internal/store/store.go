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

type ClaimMessageIdempotencyParams struct {
	ActorUserID    int64
	Operation      string
	IdempotencyKey string
	RequestHash    []byte
	MessageID      int64
	CreatedAt      int64
	ExpiresAt      int64
}

type MessageIdempotencyClaim struct {
	MessageID   int64
	RequestHash []byte
	Claimed     bool
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
	ClaimMessageIdempotency(ctx context.Context, params ClaimMessageIdempotencyParams) (*MessageIdempotencyClaim, error)
	CreateMessage(ctx context.Context, params CreateMessageParams) (*model.Message, error)
	GetMessage(ctx context.Context, messageID int64) (*model.Message, error)
	ListMessages(ctx context.Context, params ListMessagesParams) ([]*model.Message, error)
	UpdateMessage(ctx context.Context, params UpdateMessageParams) (*model.Message, error)
	DeleteMessage(ctx context.Context, messageID, actorUserID int64, hasModPermission bool) (*model.Message, error)
	ReplaceMessageMentions(ctx context.Context, messageID int64, mentions model.MessageMentions) error
	ListMessageMentions(ctx context.Context, messageID int64) (*model.MessageMentions, error)
	ListMessagesMentions(ctx context.Context, messageIDs []int64) (map[int64]*model.MessageMentions, error)
	RebuildExpandedMessageMentions(ctx context.Context, messageID, expectedRevision int64, userIDs []int64) (bool, error)
	CreateDmChannel(ctx context.Context, channel *model.DmChannel) error
	GetDmChannel(ctx context.Context, channelID int64) (*model.DmChannel, error)
	GetDmChannelByPair(ctx context.Context, userLo, userHi int64) (*model.DmChannel, error)
	ListDmChannels(ctx context.Context, params ListDmChannelsParams) ([]*model.DmChannel, error)
	ListAllDmChannels(ctx context.Context, userID int64) ([]*model.DmChannel, error)
	AckMessage(ctx context.Context, userID, channelID, messageID int64) (bool, error)
	ListReadyChannelReadStates(ctx context.Context, userID int64, channelIDs []int64) ([]*model.ChannelReadState, error)
	GetLastMessageID(ctx context.Context, channelID int64) (int64, error)
	EnsureMessageStream(ctx context.Context, streamKey string, shardID int) error
	ReserveMessageSequences(ctx context.Context, streamKey string, count int) (outbox.ReservedRange, error)
	InsertMessageOutbox(ctx context.Context, records []outbox.Record) error
	EnsureReadStateStream(ctx context.Context, streamKey string, shardID int) error
	ReserveReadStateSequences(ctx context.Context, streamKey string, count int) (outbox.ReservedRange, error)
	InsertReadStateOutbox(ctx context.Context, records []outbox.Record) error
	NotifyOutbox(ctx context.Context, channel string) error
}

type ListDmChannelsParams struct {
	UserID   int64
	BeforeID int64
	Limit    int
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
