package store

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/soasurs/cordis/services/user/v1/internal/model"
)

type userRow struct {
	UserID         int64  `db:"user_id"`
	Email          string `db:"email"`
	HashedPassword string `db:"hashed_password"`
	CreatedAt      int64  `db:"created_at"`
	UpdatedAt      int64  `db:"updated_at"`
	DeletedAt      int64  `db:"deleted_at"`
}

func (s *Store) CreateUser(ctx context.Context, userID int64, email, hashedPassword string) (*model.User, error) {
	row := &userRow{
		UserID:         userID,
		Email:          email,
		HashedPassword: hashedPassword,
		CreatedAt:      time.Now().UnixMilli(),
		UpdatedAt:      0,
		DeletedAt:      0,
	}

	_, err := sqlx.NamedExecContext(ctx, s.q, CreateUserStatement, row)
	if err != nil {
		return nil, err
	}
	return &model.User{
		UserID:         row.UserID,
		Email:          row.Email,
		HashedPassword: row.HashedPassword,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
		DeletedAt:      row.DeletedAt,
	}, nil
}

func (s *Store) GetUser(ctx context.Context, userID int64) (*model.User, error) {
	row := new(userRow)
	err := sqlx.GetContext(ctx, s.q, row, GetUserQuery, userID, 0)
	if err != nil {
		return nil, err
	}
	return &model.User{
		UserID:         row.UserID,
		Email:          row.Email,
		HashedPassword: row.HashedPassword,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
		DeletedAt:      row.DeletedAt,
	}, nil
}

func (s *Store) GetUserWithEmail(ctx context.Context, email string) (*model.User, error) {
	row := new(userRow)
	err := sqlx.GetContext(ctx, s.q, row, GetUserWithEmailQuery, email, 0)
	if err != nil {
		return nil, err
	}
	return &model.User{
		UserID:         row.UserID,
		Email:          row.Email,
		HashedPassword: row.HashedPassword,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
		DeletedAt:      row.DeletedAt,
	}, nil
}
