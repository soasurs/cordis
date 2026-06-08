package server

import (
	"context"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
)

func (s *userServer) UpdateEmail(ctx context.Context, req *userv1.UpdateEmailRequest) (*userv1.UpdateEmailResponse, error) {
	user, err := s.svcCtx.Store.UpdateUserEmail(ctx, req.GetUserId(), req.GetEmail())
	if err != nil {
		return nil, err
	}

	resp := new(userv1.UpdateEmailResponse)
	resp.SetUser(toPBUser(user))
	return resp, nil
}
