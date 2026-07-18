package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"

	"github.com/soasurs/cordis/services/user/v1/internal/model"
)

type ListRelationshipsParams struct {
	UserID         int64
	Type           int16
	BeforeTargetID int64
	Limit          int
}

type Store interface {
	Transact(ctx context.Context, fn func(txStore Store) error) error
	CreateUser(ctx context.Context, userID int64, email string) (*model.User, error)
	GetUser(ctx context.Context, userID int64) (*model.User, error)
	GetUserWithEmail(ctx context.Context, email string) (*model.User, error)
	CheckEmailAvailability(ctx context.Context, email string) (bool, error)
	UpdateUserEmail(ctx context.Context, userID int64, email string) (*model.User, error)
	MarkUserEmailVerified(ctx context.Context, userID int64, email string, verifiedAt int64) error
	CreateUserProfile(ctx context.Context, userID int64, username, name, avatarURI string) (*model.UserProfile, error)
	GetUserProfile(ctx context.Context, userID int64) (*model.UserProfile, error)
	GetUserProfileByUsername(ctx context.Context, username string) (*model.UserProfile, error)
	UpdateUserProfile(ctx context.Context, userID int64, name, avatarURI string) (*model.UserProfile, error)
	UpdateUsername(ctx context.Context, userID int64, username string) (*model.UserProfile, error)
	LockRelationshipPair(ctx context.Context, userID, targetID int64) error
	UpsertRelationship(ctx context.Context, relationship *model.Relationship) error
	GetRelationship(ctx context.Context, userID, targetID int64) (*model.Relationship, error)
	DeleteRelationship(ctx context.Context, userID, targetID int64) error
	DeleteRelationshipExceptBlocked(ctx context.Context, userID, targetID int64) error
	ListRelationships(ctx context.Context, params ListRelationshipsParams) ([]*model.Relationship, error)
	ListRelationshipsByTargets(ctx context.Context, userID int64, targetIDs []int64) ([]*model.Relationship, error)
	ListRelationshipsBidirectional(ctx context.Context, userID int64, targetIDs []int64) ([]*model.Relationship, error)
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
