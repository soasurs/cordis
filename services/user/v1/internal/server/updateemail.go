package server

import (
	"context"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
)

func (s *userServer) UpdateEmail(ctx context.Context, req *userv1.UpdateEmailRequest) (*userv1.UpdateEmailResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, errUserIDRequired
	}
	email := normalizeEmail(req.GetEmail())
	if email == "" {
		return nil, errEmailRequired
	}
	if err := isValidEmail(email); err != nil {
		return nil, err
	}
	user, err := s.svcCtx.Store.UpdateUserEmail(ctx, req.GetUserId(), email)
	if err != nil {
		return nil, mapStoreError(err)
	}

	resp := new(userv1.UpdateEmailResponse)
	resp.SetUser(userToProto(user))
	return resp, nil
}
