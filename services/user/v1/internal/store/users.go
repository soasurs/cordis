package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5"

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

	_, err := s.q.Exec(ctx, CreateUserStatement, row.UserID, row.Email, row.CreatedAt, row.UpdatedAt, row.DeletedAt)
	if err != nil {
		return nil, err
	}
	return userFromRow(row), nil
}

func (s *SQLStore) GetUser(ctx context.Context, userID int64) (*model.User, error) {
	row, err := scanOne(ctx, s.q, GetUserQuery, pgx.RowToStructByName[userRow], userID, 0)
	if err != nil {
		return nil, err
	}
	return userFromRow(&row), nil
}

func (s *SQLStore) GetUserWithEmail(ctx context.Context, email string) (*model.User, error) {
	row, err := scanOne(ctx, s.q, GetUserWithEmailQuery, pgx.RowToStructByName[userRow], email, 0)
	if err != nil {
		return nil, err
	}
	return userFromRow(&row), nil
}

func (s *SQLStore) CheckEmailAvailability(ctx context.Context, email string) (bool, error) {
	available, err := scanOne(ctx, s.q, CheckEmailAvailabilityQuery, pgx.RowTo[bool], email, 0)
	if err != nil {
		return false, err
	}
	return available, nil
}

func (s *SQLStore) UpdateUserEmail(ctx context.Context, userID int64, email string) (*model.User, error) {
	row, err := scanOne(ctx, s.q, UpdateUserEmailQuery, pgx.RowToStructByName[userRow], email, time.Now().UnixMilli(), userID, 0)
	if err != nil {
		return nil, err
	}
	return userFromRow(&row), nil
}

func (s *SQLStore) MarkUserEmailVerified(ctx context.Context, userID int64, email string, verifiedAt int64) error {
	tag, err := s.q.Exec(ctx, MarkUserEmailVerifiedStatement, verifiedAt, verifiedAt, userID, email, 0)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
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
