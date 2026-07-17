package server

import (
	"context"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
)

func (s *userServer) GetUserProfile(ctx context.Context, req *userv1.GetUserProfileRequest) (*userv1.GetUserProfileResponse, error) {
	profile, err := s.svcCtx.Store.GetUserProfile(ctx, req.GetUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}

	resp := new(userv1.GetUserProfileResponse)
	resp.SetProfile(userProfileToProto(profile))
	return resp, nil
}
