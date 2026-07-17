package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/soasurs/cordis/services/user/v1/internal/model"
)

type userRow struct {
	UserID          int64  `db:"user_id"`
	Email           string `db:"email"`
	CreatedAt       int64  `db:"created_at"`
	UpdatedAt       int64  `db:"updated_at"`
	DeletedAt       int64  `db:"deleted_at"`
	EmailVerifiedAt int64  `db:"email_verified_at"`
}

func (s *SQLStore) CreateUser(ctx context.Context, userID int64, email string) (*model.User, error) {
	row := &userRow{
		UserID:    userID,
		Email:     email,
		CreatedAt: time.Now().UnixMilli(),
		UpdatedAt: 0,
		DeletedAt: 0,
	}

	_, err := sqlx.NamedExecContext(ctx, s.q, CreateUserStatement, row)
	if err != nil {
		return nil, err
	}
	return userFromRow(row), nil
}

func (s *SQLStore) GetUser(ctx context.Context, userID int64) (*model.User, error) {
	row := new(userRow)
	err := sqlx.GetContext(ctx, s.q, row, GetUserQuery, userID, 0)
	if err != nil {
		return nil, err
	}
	return userFromRow(row), nil
}

func (s *SQLStore) GetUserWithEmail(ctx context.Context, email string) (*model.User, error) {
	row := new(userRow)
	err := sqlx.GetContext(ctx, s.q, row, GetUserWithEmailQuery, email, 0)
	if err != nil {
		return nil, err
	}
	return userFromRow(row), nil
}

func (s *SQLStore) CheckEmailAvailability(ctx context.Context, email string) (bool, error) {
	var available bool
	err := sqlx.GetContext(ctx, s.q, &available, CheckEmailAvailabilityQuery, email, 0)
	if err != nil {
		return false, err
	}
	return available, nil
}

func (s *SQLStore) UpdateUserEmail(ctx context.Context, userID int64, email string) (*model.User, error) {
	row := new(userRow)
	err := sqlx.GetContext(ctx, s.q, row, UpdateUserEmailQuery, email, time.Now().UnixMilli(), userID, 0)
	if err != nil {
		return nil, err
	}
	return userFromRow(row), nil
}

func (s *SQLStore) MarkUserEmailVerified(ctx context.Context, userID int64, email string, verifiedAt int64) error {
	res, err := s.q.ExecContext(ctx, MarkUserEmailVerifiedStatement, verifiedAt, verifiedAt, userID, email, 0)
	if err != nil {
		return err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func userFromRow(row *userRow) *model.User {
	return &model.User{
		UserID:          row.UserID,
		Email:           row.Email,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
		DeletedAt:       row.DeletedAt,
		EmailVerifiedAt: row.EmailVerifiedAt,
	}
}
