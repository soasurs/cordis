package server

import (
	"context"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/password"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *userServer) ChangePassword(ctx context.Context, req *userv1.ChangePasswordRequest) (*userv1.ChangePasswordResponse, error) {
	user, err := s.svcCtx.Store.GetUser(ctx, req.GetUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}

	ok, err := password.Verify(user.HashedPassword, req.GetOldPassword())
	if err != nil {
		return nil, err
	}
	if !ok {
		resp := new(userv1.ChangePasswordResponse)
		resp.SetOk(false)
		return resp, nil
	}

	if req.GetNewPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "new password is required")
	}

	hashedPassword, err := password.Hash(req.GetNewPassword())
	if err != nil {
		return nil, err
	}
	if err := s.svcCtx.Store.UpdateUserPassword(ctx, req.GetUserId(), hashedPassword); err != nil {
		return nil, mapStoreError(err)
	}

	resp := new(userv1.ChangePasswordResponse)
	resp.SetOk(true)
	return resp, nil
}
