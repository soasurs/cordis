package store

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/soasurs/cordis/services/user/v1/internal/model"
)

type userProfileRow struct {
	UserID    int64  `db:"user_id"`
	Name      string `db:"name"`
	AvatarURI string `db:"avatar_uri"`
	CreatedAt int64  `db:"created_at"`
	UpdatedAt int64  `db:"updated_at"`
	DeletedAt int64  `db:"deleted_at"`
}

func (s *Store) CreateUserProfile(ctx context.Context, userID int64, name, avatarURI string) (*model.UserProfile, error) {
	row := &userProfileRow{
		UserID:    userID,
		Name:      name,
		AvatarURI: avatarURI,
		CreatedAt: time.Now().UnixMilli(),
		UpdatedAt: 0,
		DeletedAt: 0,
	}

	_, err := sqlx.NamedExecContext(ctx, s.q, CreateUserProfileStatement, row)
	if err != nil {
		return nil, err
	}

	return &model.UserProfile{
		UserID:    row.UserID,
		Name:      row.Name,
		AvatarURI: row.AvatarURI,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		DeletedAt: row.DeletedAt,
	}, nil
}

func (s *Store) GetUserProfile(ctx context.Context, userID int64) (*model.UserProfile, error) {
	row := new(userProfileRow)
	err := sqlx.GetContext(ctx, s.q, row, GetUserProfileQuery, userID, 0)
	if err != nil {
		return nil, err
	}
	return &model.UserProfile{
		UserID:    row.UserID,
		Name:      row.Name,
		AvatarURI: row.AvatarURI,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		DeletedAt: row.DeletedAt,
	}, nil
}
