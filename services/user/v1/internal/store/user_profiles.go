package store

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"github.com/soasurs/cordis/services/user/v1/internal/model"
)

type userProfileRow struct {
	UserID    int64  `db:"user_id"`
	Username  string `db:"username"`
	Name      string `db:"name"`
	AvatarURI string `db:"avatar_uri"`
	CreatedAt int64  `db:"created_at"`
	UpdatedAt int64  `db:"updated_at"`
	DeletedAt int64  `db:"deleted_at"`
}

func (s *SQLStore) CreateUserProfile(ctx context.Context, userID int64, username, name, avatarURI string) (*model.UserProfile, error) {
	row := &userProfileRow{
		UserID:    userID,
		Username:  username,
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
		Username:  row.Username,
		Name:      row.Name,
		AvatarURI: row.AvatarURI,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		DeletedAt: row.DeletedAt,
	}, nil
}

func (s *SQLStore) GetUserProfile(ctx context.Context, userID int64) (*model.UserProfile, error) {
	row := new(userProfileRow)
	err := sqlx.GetContext(ctx, s.q, row, GetUserProfileQuery, userID, 0)
	if err != nil {
		return nil, err
	}
	return &model.UserProfile{
		UserID:    row.UserID,
		Username:  row.Username,
		Name:      row.Name,
		AvatarURI: row.AvatarURI,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		DeletedAt: row.DeletedAt,
	}, nil
}

func (s *SQLStore) ListUserProfiles(ctx context.Context, userIDs []int64) ([]*model.UserProfile, error) {
	var rows []*userProfileRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, ListUserProfilesQuery, pq.Array(userIDs), 0); err != nil {
		return nil, err
	}
	profiles := make([]*model.UserProfile, 0, len(rows))
	for _, row := range rows {
		profiles = append(profiles, &model.UserProfile{
			UserID:    row.UserID,
			Username:  row.Username,
			Name:      row.Name,
			AvatarURI: row.AvatarURI,
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
			DeletedAt: row.DeletedAt,
		})
	}
	return profiles, nil
}

func (s *SQLStore) UpdateUserProfile(ctx context.Context, params UpdateUserProfileParams) (*model.UserProfile, error) {
	var name, avatarURI string
	if params.Name != nil {
		name = *params.Name
	}
	if params.AvatarURI != nil {
		avatarURI = *params.AvatarURI
	}
	row := new(userProfileRow)
	err := sqlx.GetContext(
		ctx,
		s.q,
		row,
		UpdateUserProfileQuery,
		params.Name != nil,
		name,
		params.AvatarURI != nil,
		avatarURI,
		time.Now().UnixMilli(),
		params.UserID,
		0,
	)
	if err != nil {
		return nil, err
	}
	return &model.UserProfile{
		UserID:    row.UserID,
		Username:  row.Username,
		Name:      row.Name,
		AvatarURI: row.AvatarURI,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		DeletedAt: row.DeletedAt,
	}, nil
}

func (s *SQLStore) GetUserProfileByUsername(ctx context.Context, username string) (*model.UserProfile, error) {
	row := new(userProfileRow)
	if err := sqlx.GetContext(ctx, s.q, row, GetUserProfileByUsernameQuery, username, 0); err != nil {
		return nil, err
	}
	return &model.UserProfile{
		UserID:    row.UserID,
		Username:  row.Username,
		Name:      row.Name,
		AvatarURI: row.AvatarURI,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		DeletedAt: row.DeletedAt,
	}, nil
}

func (s *SQLStore) UpdateUsername(ctx context.Context, userID int64, username string) (*model.UserProfile, error) {
	row := new(userProfileRow)
	if err := sqlx.GetContext(ctx, s.q, row, UpdateUsernameQuery, username, time.Now().UnixMilli(), userID, 0); err != nil {
		return nil, err
	}
	return &model.UserProfile{
		UserID:    row.UserID,
		Username:  row.Username,
		Name:      row.Name,
		AvatarURI: row.AvatarURI,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		DeletedAt: row.DeletedAt,
	}, nil
}
