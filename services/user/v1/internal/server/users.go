package server

import (
	"context"
	"strings"
	"time"

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
	username := normalizeUsername(req.GetUsername())
	if err := validateUsername(username); err != nil {
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

		if _, err := txStore.CreateUserProfile(ctx, userID, username, req.GetName()); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if isUsernameViolation(err) {
			return nil, rpcerror.New(codes.AlreadyExists, rpcerror.UserDomain, rpcerror.UserUsernameTaken, "username is already taken")
		}
		if isUniqueViolation(err) {
			return nil, rpcerror.New(codes.AlreadyExists, rpcerror.UserDomain, rpcerror.UserEmailAlreadyExists, "email already exists")
		}
		return nil, err
	}

	resp := &userv1.CreateUserResponse{}
	resp.SetUser(userToProto(user))
	return resp, nil
}

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

func (s *userServer) CheckEmailAvailability(ctx context.Context, req *userv1.CheckEmailAvailabilityRequest) (*userv1.CheckEmailAvailabilityResponse, error) {
	available, err := s.svcCtx.Store.CheckEmailAvailability(ctx, normalizeEmail(req.GetEmail()))
	if err != nil {
		return nil, err
	}

	resp := new(userv1.CheckEmailAvailabilityResponse)
	resp.SetAvailable(available)
	return resp, nil
}

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

func (s *userServer) MarkEmailVerified(ctx context.Context, req *userv1.MarkEmailVerifiedRequest) (*userv1.MarkEmailVerifiedResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, errUserIDRequired
	}
	if strings.TrimSpace(req.GetEmail()) == "" {
		return nil, errEmailRequired
	}

	verifiedAt := req.GetVerifiedAt()
	if verifiedAt <= 0 {
		verifiedAt = time.Now().UnixMilli()
	}
	// The email predicate keeps stale verification tokens from confirming an
	// address the user has since replaced.
	if err := s.svcCtx.Store.MarkUserEmailVerified(ctx, req.GetUserId(), req.GetEmail(), verifiedAt); err != nil {
		return nil, mapStoreError(err)
	}

	resp := new(userv1.MarkEmailVerifiedResponse)
	resp.SetOk(true)
	return resp, nil
}
