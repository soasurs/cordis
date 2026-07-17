package server

import (
	"context"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
)

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
