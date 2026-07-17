package server

import (
	"context"
	"errors"

	"github.com/lib/pq"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/user/v1/internal/model"
	"github.com/soasurs/cordis/services/user/v1/internal/store"
)

func (s *userServer) CreateUser(ctx context.Context, req *userv1.CreateUserRequest) (*userv1.CreateUserResponse, error) {
	if err := validateName(req.GetName()); err != nil {
		return nil, err
	}
	email := normalizeEmail(req.GetEmail())
	if email == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}
	if err := isValidEmail(email); err != nil {
		return nil, err
	}
	userID := s.svcCtx.Snowflake.Generate().Int64()

	var user *model.User
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		createdUser, err := txStore.CreateUser(ctx, userID, email)
		if err != nil {
			return err
		}
		user = createdUser

		if _, err := txStore.CreateUserProfile(ctx, userID, req.GetName(), ""); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, rpcerror.New(codes.AlreadyExists, rpcerror.UserDomain, rpcerror.UserEmailAlreadyExists, "email already exists")
		}
		return nil, err
	}

	resp := &userv1.CreateUserResponse{}
	resp.SetUser(userToProto(user))
	return resp, nil
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}
