package server

import (
	"context"
	"strings"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
)

func (s *userServer) UpdateEmail(ctx context.Context, req *userv1.UpdateEmailRequest) (*userv1.UpdateEmailResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, errUserIDRequired
	}
	if strings.TrimSpace(req.GetEmail()) == "" {
		return nil, errEmailRequired
	}
	if err := isValidEmail(req.GetEmail()); err != nil {
		return nil, err
	}
	user, err := s.svcCtx.Store.UpdateUserEmail(ctx, req.GetUserId(), req.GetEmail())
	if err != nil {
		return nil, mapStoreError(err)
	}

	resp := new(userv1.UpdateEmailResponse)
	resp.SetUser(toPBUser(user))
	return resp, nil
}
