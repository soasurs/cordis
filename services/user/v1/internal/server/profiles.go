package server

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
)

const maxUserProfileBatch = 100

func (s *userServer) GetUserProfile(ctx context.Context, req *userv1.GetUserProfileRequest) (*userv1.GetUserProfileResponse, error) {
	profile, err := s.svcCtx.Store.GetUserProfile(ctx, req.GetUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}

	resp := new(userv1.GetUserProfileResponse)
	resp.SetProfile(userProfileToProto(profile))
	return resp, nil
}

func (s *userServer) BatchGetUserProfiles(ctx context.Context, req *userv1.BatchGetUserProfilesRequest) (*userv1.BatchGetUserProfilesResponse, error) {
	userIDs := req.GetUserIds()
	if len(userIDs) > maxUserProfileBatch {
		return nil, errProfileBatchTooLarge
	}
	uniqueUserIDs := make([]int64, 0, len(userIDs))
	seen := make(map[int64]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID <= 0 {
			return nil, errUserIDRequired
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		uniqueUserIDs = append(uniqueUserIDs, userID)
	}

	resp := new(userv1.BatchGetUserProfilesResponse)
	if len(uniqueUserIDs) == 0 {
		return resp, nil
	}
	profiles, err := s.svcCtx.Store.ListUserProfiles(ctx, uniqueUserIDs)
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp.SetProfiles(userProfilesToProto(profiles))
	return resp, nil
}

func (s *userServer) GetUserProfileByUsername(ctx context.Context, req *userv1.GetUserProfileByUsernameRequest) (*userv1.GetUserProfileByUsernameResponse, error) {
	username := normalizeUsername(req.GetUsername())
	if err := validateUsername(username); err != nil {
		return nil, err
	}

	profile, err := s.svcCtx.Store.GetUserProfileByUsername(ctx, username)
	if err != nil {
		return nil, mapStoreError(err)
	}

	resp := new(userv1.GetUserProfileByUsernameResponse)
	resp.SetProfile(userProfileToProto(profile))
	return resp, nil
}

func (s *userServer) UpdateUserProfile(ctx context.Context, req *userv1.UpdateUserProfileRequest) (*userv1.UpdateUserProfileResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, errUserIDRequired
	}
	name := strings.TrimSpace(req.GetName())
	if name == "" {
		return nil, errNameRequired
	}
	if len(name) > maxNameLength {
		return nil, errNameTooLong
	}

	profile, err := s.svcCtx.Store.UpdateUserProfile(ctx, req.GetUserId(), name, req.GetAvatarUri())
	if err != nil {
		return nil, mapStoreError(err)
	}

	resp := new(userv1.UpdateUserProfileResponse)
	resp.SetProfile(userProfileToProto(profile))
	return resp, nil
}

func (s *userServer) UpdateUsername(ctx context.Context, req *userv1.UpdateUsernameRequest) (*userv1.UpdateUsernameResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, errUserIDRequired
	}
	username := normalizeUsername(req.GetUsername())
	if err := validateUsername(username); err != nil {
		return nil, err
	}

	profile, err := s.svcCtx.Store.UpdateUsername(ctx, req.GetUserId(), username)
	if err != nil {
		if isUsernameViolation(err) {
			return nil, rpcerror.New(codes.AlreadyExists, rpcerror.UserDomain, rpcerror.UserUsernameTaken, "username is already taken")
		}
		return nil, mapStoreError(err)
	}

	resp := new(userv1.UpdateUsernameResponse)
	resp.SetProfile(userProfileToProto(profile))
	return resp, nil
}
