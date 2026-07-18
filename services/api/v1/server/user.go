package server

import (
	"context"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	apiratelimit "github.com/soasurs/cordis/services/api/v1/ratelimit"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

type userServer struct {
	svcCtx *svc.ServiceContext
}

func NewUser(svcCtx *svc.ServiceContext) apiv1connect.UserServiceHandler {
	return &userServer{svcCtx: svcCtx}
}

func (s *userServer) GetCurrentUser(ctx context.Context, _ *apiv1.GetCurrentUserRequest) (*apiv1.GetCurrentUserResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}

	userReq := new(userv1.GetUserRequest)
	userReq.SetUserId(auth.GetUserId())
	userResp, err := s.svcCtx.UserClient.GetUser(ctx, userReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	profileReq := new(userv1.GetUserProfileRequest)
	profileReq.SetUserId(auth.GetUserId())
	profileResp, err := s.svcCtx.UserClient.GetUserProfile(ctx, profileReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	return &apiv1.GetCurrentUserResponse{
		User:    userToAPI(userResp.GetUser()),
		Profile: userProfileToAPI(profileResp.GetProfile()),
	}, nil
}

func (s *userServer) GetUserProfile(ctx context.Context, req *apiv1.GetUserProfileRequest) (*apiv1.GetUserProfileResponse, error) {
	if err := apiratelimit.CheckIP(ctx, apiratelimit.PolicyGetUserProfileIP); err != nil {
		return nil, err
	}
	svcReq := new(userv1.GetUserProfileRequest)
	svcReq.SetUserId(req.GetUserId())
	svcResp, err := s.svcCtx.UserClient.GetUserProfile(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.GetUserProfileResponse{
		Profile: userProfileToAPI(svcResp.GetProfile()),
	}, nil
}

func (s *userServer) CheckEmailAvailability(ctx context.Context, req *apiv1.CheckEmailAvailabilityRequest) (*apiv1.CheckEmailAvailabilityResponse, error) {
	if err := apiratelimit.CheckIP(ctx, apiratelimit.PolicyCheckEmailAvailabilityIP); err != nil {
		return nil, err
	}
	svcReq := new(userv1.CheckEmailAvailabilityRequest)
	svcReq.SetEmail(req.GetEmail())
	svcResp, err := s.svcCtx.UserClient.CheckEmailAvailability(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.CheckEmailAvailabilityResponse{
		Available: new(svcResp.GetAvailable()),
	}, nil
}

func (s *userServer) UpdateEmail(ctx context.Context, req *apiv1.UpdateEmailRequest) (*apiv1.UpdateEmailResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}

	svcReq := new(userv1.UpdateEmailRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetEmail(req.GetEmail())
	svcResp, err := s.svcCtx.UserClient.UpdateEmail(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.UpdateEmailResponse{
		User: userToAPI(svcResp.GetUser()),
	}, nil
}

func (s *userServer) UpdateUserProfile(ctx context.Context, req *apiv1.UpdateUserProfileRequest) (*apiv1.UpdateUserProfileResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}

	svcReq := new(userv1.UpdateUserProfileRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetName(req.GetName())
	svcReq.SetAvatarUri(req.GetAvatarUri())
	svcResp, err := s.svcCtx.UserClient.UpdateUserProfile(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.UpdateUserProfileResponse{
		Profile: userProfileToAPI(svcResp.GetProfile()),
	}, nil
}

func (s *userServer) ChangePassword(ctx context.Context, req *apiv1.ChangePasswordRequest) (*apiv1.ChangePasswordResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}

	// Credentials are owned by the authenticator, which verifies the old
	// password, replaces it, and revokes the other sessions atomically.
	svcReq := new(authenticatorv1.ChangePasswordRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetCurrentSessionId(auth.GetSessionId())
	svcReq.SetOldPassword(req.GetOldPassword())
	svcReq.SetNewPassword(req.GetNewPassword())
	svcResp, err := s.svcCtx.AuthenticatorClient.ChangePassword(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.ChangePasswordResponse{
		Ok: new(svcResp.GetOk()),
	}, nil
}

func (s *userServer) UpdateUsername(ctx context.Context, req *apiv1.UpdateUsernameRequest) (*apiv1.UpdateUsernameResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}

	svcReq := new(userv1.UpdateUsernameRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetUsername(req.GetUsername())
	svcResp, err := s.svcCtx.UserClient.UpdateUsername(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.UpdateUsernameResponse{Profile: userProfileToAPI(svcResp.GetProfile())}, nil
}

func userToAPI(user *userv1.User) *apiv1.User {
	if user == nil {
		return nil
	}
	return &apiv1.User{
		UserId:          new(user.GetUserId()),
		Email:           new(user.GetEmail()),
		CreatedAt:       new(user.GetCreatedAt()),
		UpdatedAt:       new(user.GetUpdatedAt()),
		EmailVerifiedAt: new(user.GetEmailVerifiedAt()),
	}
}

func userProfileToAPI(profile *userv1.UserProfile) *apiv1.UserProfile {
	if profile == nil {
		return nil
	}
	return &apiv1.UserProfile{
		UserId:    new(profile.GetUserId()),
		Username:  new(profile.GetUsername()),
		Name:      new(profile.GetName()),
		AvatarUri: new(profile.GetAvatarUri()),
		CreatedAt: new(profile.GetCreatedAt()),
		UpdatedAt: new(profile.GetUpdatedAt()),
	}
}
