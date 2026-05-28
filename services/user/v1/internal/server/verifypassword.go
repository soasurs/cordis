package server

import (
	"context"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/password"
)

func (s *userServer) VerifyPassword(ctx context.Context, req *userv1.VerifyPasswordRequest) (*userv1.VerifyPasswordResponse, error) {
	user, err := s.svcCtx.Store.GetUserWithEmail(ctx, req.GetEmail())
	if err != nil {
		return nil, err
	}

	ok, err := password.Verify(user.HashedPassword, req.GetPassword())
	if err != nil {
		return nil, err
	}

	resp := new(userv1.VerifyPasswordResponse)
	resp.SetOk(ok)
	resp.SetRequireChallenge(false)
	return resp, nil
}
