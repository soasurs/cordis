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

	resp := new(apiv1.GetCurrentUserResponse)
	resp.SetUser(userToAPI(userResp.GetUser()))
	resp.SetProfile(userProfileToAPI(profileResp.GetProfile()))
	return resp, nil
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
	resp := new(apiv1.GetUserProfileResponse)
	resp.SetProfile(userProfileToAPI(svcResp.GetProfile()))
	return resp, nil
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
	resp := new(apiv1.CheckEmailAvailabilityResponse)
	resp.SetAvailable(svcResp.GetAvailable())
	return resp, nil
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
	resp := new(apiv1.UpdateEmailResponse)
	resp.SetUser(userToAPI(svcResp.GetUser()))
	return resp, nil
}

func (s *userServer) UpdateUserProfile(ctx context.Context, req *apiv1.UpdateUserProfileRequest) (*apiv1.UpdateUserProfileResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}

	svcReq := new(userv1.UpdateUserProfileRequest)
	svcReq.SetUserId(auth.GetUserId())
	if req.HasName() {
		svcReq.SetName(req.GetName())
	}
	if req.HasAvatarUri() {
		svcReq.SetAvatarUri(req.GetAvatarUri())
	}
	svcResp, err := s.svcCtx.UserClient.UpdateUserProfile(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.UpdateUserProfileResponse)
	resp.SetProfile(userProfileToAPI(svcResp.GetProfile()))
	return resp, nil
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
	resp := new(apiv1.ChangePasswordResponse)
	resp.SetOk(svcResp.GetOk())
	return resp, nil
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
	resp := new(apiv1.UpdateUsernameResponse)
	resp.SetProfile(userProfileToAPI(svcResp.GetProfile()))
	return resp, nil
}

func userToAPI(user *userv1.User) *apiv1.User {
	if user == nil {
		return nil
	}
	resp := new(apiv1.User)
	resp.SetUserId(user.GetUserId())
	resp.SetEmail(user.GetEmail())
	resp.SetCreatedAt(user.GetCreatedAt())
	resp.SetUpdatedAt(user.GetUpdatedAt())
	resp.SetEmailVerifiedAt(user.GetEmailVerifiedAt())
	return resp
}

func userProfileToAPI(profile *userv1.UserProfile) *apiv1.UserProfile {
	if profile == nil {
		return nil
	}
	resp := new(apiv1.UserProfile)
	resp.SetUserId(profile.GetUserId())
	resp.SetUsername(profile.GetUsername())
	resp.SetName(profile.GetName())
	resp.SetAvatarUri(profile.GetAvatarUri())
	resp.SetCreatedAt(profile.GetCreatedAt())
	resp.SetUpdatedAt(profile.GetUpdatedAt())
	return resp
}
