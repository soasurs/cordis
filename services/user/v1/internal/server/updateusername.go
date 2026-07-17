package server

import (
	"context"

	"google.golang.org/grpc/codes"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
)

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
