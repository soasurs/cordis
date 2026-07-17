package server

import (
	"context"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
)

func (s *userServer) GetUser(ctx context.Context, req *userv1.GetUserRequest) (*userv1.GetUserResponse, error) {
	switch req.WhichIdentity() {
	case userv1.GetUserRequest_UserId_case:
		return s.getUserWithUserID(ctx, req.GetUserId())
	case userv1.GetUserRequest_Email_case:
		return s.getUserWithEmail(ctx, req.GetEmail())
	default:
		return nil, nil
	}
}

func (s *userServer) getUserWithUserID(ctx context.Context, userID int64) (*userv1.GetUserResponse, error) {
	user, err := s.svcCtx.Store.GetUser(ctx, userID)
	if err != nil {
		return nil, mapStoreError(err)
	}

	resp := new(userv1.GetUserResponse)
	resp.SetUser(userToProto(user))
	return resp, nil
}

func (s *userServer) getUserWithEmail(ctx context.Context, email string) (*userv1.GetUserResponse, error) {
	user, err := s.svcCtx.Store.GetUserWithEmail(ctx, normalizeEmail(email))
	if err != nil {
		return nil, mapStoreError(err)
	}

	resp := new(userv1.GetUserResponse)
	resp.SetUser(userToProto(user))
	return resp, nil
}
