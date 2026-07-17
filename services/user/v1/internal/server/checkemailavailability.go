package server

import (
	"context"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
)

func (s *userServer) CheckEmailAvailability(ctx context.Context, req *userv1.CheckEmailAvailabilityRequest) (*userv1.CheckEmailAvailabilityResponse, error) {
	available, err := s.svcCtx.Store.CheckEmailAvailability(ctx, normalizeEmail(req.GetEmail()))
	if err != nil {
		return nil, err
	}

	resp := new(userv1.CheckEmailAvailabilityResponse)
	resp.SetAvailable(available)
	return resp, nil
}
