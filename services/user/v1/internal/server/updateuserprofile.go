package server

import (
	"context"
	"strings"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
)

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
	resp.SetProfile(toPBUserProfile(profile))
	return resp, nil
}
