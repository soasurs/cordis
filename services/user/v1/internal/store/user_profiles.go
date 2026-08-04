package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/soasurs/cordis/services/user/v1/internal/model"
)

type userProfileRow struct {
	UserID        int64  `db:"user_id"`
	Username      string `db:"username"`
	Name          string `db:"name"`
	Bio           string `db:"bio"`
	AvatarAssetID int64  `db:"avatar_asset_id"`
	CreatedAt     int64  `db:"created_at"`
	UpdatedAt     int64  `db:"updated_at"`
	DeletedAt     int64  `db:"deleted_at"`
}

func profileFromRow(row *userProfileRow) *model.UserProfile {
	return &model.UserProfile{
		UserID:        row.UserID,
		Username:      row.Username,
		Name:          row.Name,
		Bio:           row.Bio,
		AvatarAssetID: row.AvatarAssetID,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
		DeletedAt:     row.DeletedAt,
	}
}

func (s *SQLStore) CreateUserProfile(ctx context.Context, userID int64, username, name string) (*model.UserProfile, error) {
	row := &userProfileRow{
		UserID:    userID,
		Username:  username,
		Name:      name,
		CreatedAt: time.Now().UnixMilli(),
	}

	_, err := s.q.Exec(
		ctx,
		CreateUserProfileStatement,
		row.UserID,
		row.Username,
		row.Name,
		row.Bio,
		row.AvatarAssetID,
		row.CreatedAt,
		row.UpdatedAt,
		row.DeletedAt,
	)
	if err != nil {
		return nil, err
	}

	return profileFromRow(row), nil
}

func (s *SQLStore) GetUserProfile(ctx context.Context, userID int64) (*model.UserProfile, error) {
	row, err := scanOne(ctx, s.q, GetUserProfileQuery, pgx.RowToStructByName[userProfileRow], userID, 0)
	if err != nil {
		return nil, err
	}
	return profileFromRow(&row), nil
}

func (s *SQLStore) ListUserProfiles(ctx context.Context, userIDs []int64) ([]*model.UserProfile, error) {
	rows, err := scanMany(ctx, s.q, ListUserProfilesQuery, pgx.RowToStructByName[userProfileRow], userIDs, 0)
	if err != nil {
		return nil, err
	}
	profiles := make([]*model.UserProfile, 0, len(rows))
	for i := range rows {
		profiles = append(profiles, profileFromRow(&rows[i]))
	}
	return profiles, nil
}

func (s *SQLStore) UpdateUserProfile(ctx context.Context, params UpdateUserProfileParams) (*model.UserProfile, error) {
	var name string
	if params.Name != nil {
		name = *params.Name
	}
	var bio string
	if params.Bio != nil {
		bio = *params.Bio
	}
	var avatarAssetID int64
	if params.AvatarAssetID != nil {
		avatarAssetID = *params.AvatarAssetID
	}
	row, err := scanOne(
		ctx,
		s.q,
		UpdateUserProfileQuery,
		pgx.RowToStructByName[userProfileRow],
		params.Name != nil,
		name,
		params.Bio != nil,
		bio,
		params.AvatarAssetID != nil,
		avatarAssetID,
		time.Now().UnixMilli(),
		params.UserID,
		0,
	)
	if err != nil {
		return nil, err
	}
	return profileFromRow(&row), nil
}

func (s *SQLStore) UpdateUserAvatar(ctx context.Context, userID, assetID int64) (*model.UserProfile, error) {
	row, err := scanOne(
		ctx,
		s.q,
		UpdateUserAvatarQuery,
		pgx.RowToStructByName[userProfileRow],
		assetID,
		time.Now().UnixMilli(),
		userID,
	)
	if err != nil {
		return nil, err
	}
	return profileFromRow(&row), nil
}

func (s *SQLStore) GetUserProfileByUsername(ctx context.Context, username string) (*model.UserProfile, error) {
	row, err := scanOne(ctx, s.q, GetUserProfileByUsernameQuery, pgx.RowToStructByName[userProfileRow], username, 0)
	if err != nil {
		return nil, err
	}
	return profileFromRow(&row), nil
}

func (s *SQLStore) CheckUsernameAvailability(ctx context.Context, username string) (bool, error) {
	available, err := scanOne(ctx, s.q, CheckUsernameAvailabilityQuery, pgx.RowTo[bool], username, 0)
	if err != nil {
		return false, err
	}
	return available, nil
}

func (s *SQLStore) UpdateUsername(ctx context.Context, userID int64, username string) (*model.UserProfile, error) {
	row, err := scanOne(ctx, s.q, UpdateUsernameQuery, pgx.RowToStructByName[userProfileRow], username, time.Now().UnixMilli(), userID, 0)
	if err != nil {
		return nil, err
	}
	return profileFromRow(&row), nil
}
