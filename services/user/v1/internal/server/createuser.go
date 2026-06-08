package server

import (
	"context"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/password"
	"github.com/soasurs/cordis/services/user/v1/internal/model"
	"github.com/soasurs/cordis/services/user/v1/internal/store"
)

func (s *userServer) CreateUser(ctx context.Context, req *userv1.CreateUserRequest) (*userv1.CreateUserResponse, error) {
	userID := s.svcCtx.Snowflake.Generate().Int64()
	hashedPassword, err := password.Hash(req.GetPassword())
	if err != nil {
		return nil, err
	}

	var user *model.User
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		createdUser, err := txStore.CreateUser(ctx, userID, req.GetEmail(), hashedPassword)
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
		return nil, err
	}

	resp := &userv1.CreateUserResponse{}
	resp.SetUser(toPBUser(user))
	return resp, nil
}
