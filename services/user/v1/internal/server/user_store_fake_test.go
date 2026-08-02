package server

import (
	"context"
	"database/sql"
	"errors"

	"github.com/soasurs/cordis/services/user/v1/internal/model"
	"github.com/soasurs/cordis/services/user/v1/internal/store"
)

func (s *fakeStore) CreateUser(_ context.Context, userID int64, email string) (*model.User, error) {
	if s.createUserErr != nil {
		return nil, s.createUserErr
	}
	s.user = &model.User{
		UserID: userID,
		Email:  email,
	}
	return s.user, nil
}

func (s *fakeStore) GetUser(_ context.Context, userID int64) (*model.User, error) {
	if s.user == nil || s.user.UserID != userID {
		return nil, sql.ErrNoRows
	}
	return s.user, nil
}

func (s *fakeStore) GetUserWithEmail(_ context.Context, email string) (*model.User, error) {
	if s.getUserWithEmailErr != nil {
		return nil, s.getUserWithEmailErr
	}
	if s.user == nil || s.user.Email != email {
		return nil, sql.ErrNoRows
	}
	return s.user, nil
}

func (s *fakeStore) CheckEmailAvailability(context.Context, string) (bool, error) {
	return s.emailAvailable, nil
}

func (s *fakeStore) CheckUsernameAvailability(context.Context, string) (bool, error) {
	return s.usernameAvailable, nil
}

func (s *fakeStore) UpdateUserEmail(_ context.Context, userID int64, email string) (*model.User, error) {
	if s.user == nil || s.user.UserID != userID {
		return nil, sql.ErrNoRows
	}
	if s.user.Email != email {
		s.user.Email = email
		s.user.EmailVerifiedAt = 0
	}
	return s.user, nil
}

func (s *fakeStore) MarkUserEmailVerified(_ context.Context, userID int64, email string, verifiedAt int64) error {
	if s.user == nil || s.user.UserID != userID || s.user.Email != email {
		return sql.ErrNoRows
	}
	s.user.EmailVerifiedAt = verifiedAt
	return nil
}

func (s *fakeStore) CreateUserProfile(_ context.Context, userID int64, username, name string) (*model.UserProfile, error) {
	if s.createProfileErr != nil {
		return nil, s.createProfileErr
	}
	if s.user == nil || s.user.UserID != userID {
		return nil, errors.New("missing user")
	}
	s.profile = &model.UserProfile{
		UserID:   userID,
		Username: username,
		Name:     name,
	}
	return s.profile, nil
}

func (s *fakeStore) GetUserProfile(_ context.Context, userID int64) (*model.UserProfile, error) {
	if profile := s.eventProfiles[userID]; profile != nil {
		return profile, nil
	}
	if s.profile == nil || s.profile.UserID != userID {
		return nil, sql.ErrNoRows
	}
	return s.profile, nil
}

func (s *fakeStore) ListUserProfiles(_ context.Context, userIDs []int64) ([]*model.UserProfile, error) {
	s.listProfileIDs = append([]int64(nil), userIDs...)
	return s.batchProfiles, nil
}

func (s *fakeStore) UpdateUsername(_ context.Context, userID int64, username string) (*model.UserProfile, error) {
	if s.profile == nil || s.profile.UserID != userID {
		return nil, sql.ErrNoRows
	}
	if s.updateUsernameErr != nil {
		return nil, s.updateUsernameErr
	}
	s.profile.Username = username
	return s.profile, nil
}

func (s *fakeStore) GetUserProfileByUsername(_ context.Context, username string) (*model.UserProfile, error) {
	if s.profile == nil || s.profile.Username == "" || s.profile.Username != username {
		return nil, sql.ErrNoRows
	}
	return s.profile, nil
}

func (s *fakeStore) UpdateUserProfile(_ context.Context, params store.UpdateUserProfileParams) (*model.UserProfile, error) {
	if s.profile == nil || s.profile.UserID != params.UserID {
		return nil, sql.ErrNoRows
	}
	if params.Name != nil {
		s.profile.Name = *params.Name
	}
	if params.Bio != nil {
		s.profile.Bio = *params.Bio
	}
	if params.AvatarAssetID != nil {
		s.profile.AvatarAssetID = *params.AvatarAssetID
	}
	return s.profile, nil
}

func (s *fakeStore) UpdateUserAvatar(_ context.Context, userID, assetID int64) (*model.UserProfile, error) {
	if s.profile == nil || s.profile.UserID != userID {
		return nil, sql.ErrNoRows
	}
	s.profile.AvatarAssetID = assetID
	return s.profile, nil
}
