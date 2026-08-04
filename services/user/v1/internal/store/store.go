package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/soasurs/cordis/pkg/outbox"
	"github.com/soasurs/cordis/services/user/v1/internal/model"
)

type ListRelationshipsParams struct {
	UserID          int64
	Type            int16
	BeforeCreatedAt int64
	BeforeTargetID  int64
	Limit           int
}

type UpdateUserProfileParams struct {
	UserID        int64
	Name          *string
	Bio           *string
	AvatarAssetID *int64
}

type Store interface {
	Transact(ctx context.Context, fn func(txStore Store) error) error
	CreateUser(ctx context.Context, userID int64, email string) (*model.User, error)
	GetUser(ctx context.Context, userID int64) (*model.User, error)
	GetUserWithEmail(ctx context.Context, email string) (*model.User, error)
	CheckEmailAvailability(ctx context.Context, email string) (bool, error)
	CheckUsernameAvailability(ctx context.Context, username string) (bool, error)
	UpdateUserEmail(ctx context.Context, userID int64, email string) (*model.User, error)
	MarkUserEmailVerified(ctx context.Context, userID int64, email string, verifiedAt int64) error
	CreateUserProfile(ctx context.Context, userID int64, username, name string) (*model.UserProfile, error)
	GetUserProfile(ctx context.Context, userID int64) (*model.UserProfile, error)
	ListUserProfiles(ctx context.Context, userIDs []int64) ([]*model.UserProfile, error)
	GetUserProfileByUsername(ctx context.Context, username string) (*model.UserProfile, error)
	UpdateUserProfile(ctx context.Context, params UpdateUserProfileParams) (*model.UserProfile, error)
	UpdateUserAvatar(ctx context.Context, userID, assetID int64) (*model.UserProfile, error)
	UpdateUsername(ctx context.Context, userID int64, username string) (*model.UserProfile, error)
	LockRelationshipPair(ctx context.Context, userID, targetID int64) error
	UpsertRelationship(ctx context.Context, relationship *model.Relationship) error
	GetRelationship(ctx context.Context, userID, targetID int64) (*model.Relationship, error)
	DeleteRelationship(ctx context.Context, userID, targetID int64) error
	DeleteRelationshipExceptBlocked(ctx context.Context, userID, targetID int64) error
	ListRelationships(ctx context.Context, params ListRelationshipsParams) ([]*model.Relationship, error)
	ListRelationshipsByTargets(ctx context.Context, userID int64, targetIDs []int64) ([]*model.Relationship, error)
	ListRelationshipsBidirectional(ctx context.Context, userID int64, targetIDs []int64) ([]*model.Relationship, error)
	EnsureUserStream(ctx context.Context, streamKey string, shardID int) error
	ReserveUserSequences(ctx context.Context, streamKey string, count int) (outbox.ReservedRange, error)
	InsertUserOutbox(ctx context.Context, records []outbox.Record) error
	NotifyOutbox(ctx context.Context, channel string) error
}

type SQLStore struct {
	db *pgxpool.Pool
	q  queryer
}

// queryer is the pgx-native query surface used by SQLStore. The top-level
// store uses the pool; transactions replace it with the pgx transaction.
type queryer interface {
	Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, query string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) pgx.Row
}

func New(db *pgxpool.Pool) Store {
	return &SQLStore{
		db: db,
		q:  db,
	}
}

func (s *SQLStore) Transact(ctx context.Context, fn func(txStore Store) error) (err error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	err = fn(&SQLStore{db: s.db, q: tx})
	if err != nil {
		return
	}
	err = tx.Commit(ctx)
	return
}
