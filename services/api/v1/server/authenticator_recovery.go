package server

import (
	"context"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	apiratelimit "github.com/soasurs/cordis/services/api/v1/ratelimit"
)

func (s *authenticatorServer) RequestPasswordReset(ctx context.Context, req *apiv1.RequestPasswordResetRequest) (*apiv1.RequestPasswordResetResponse, error) {
	if err := apiratelimit.CheckIP(ctx, apiratelimit.PolicyRecoveryRequestIP); err != nil {
		return nil, err
	}
	svcReq := new(authenticatorv1.RequestPasswordResetRequest)
	svcReq.SetEmail(req.GetEmail())

	svcResp, err := s.svcCtx.AuthenticatorClient.RequestPasswordReset(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	return &apiv1.RequestPasswordResetResponse{
		Ok: new(svcResp.GetOk()),
	}, nil
}

func (s *authenticatorServer) ConfirmPasswordReset(ctx context.Context, req *apiv1.ConfirmPasswordResetRequest) (*apiv1.ConfirmPasswordResetResponse, error) {
	if err := apiratelimit.CheckIP(ctx, apiratelimit.PolicyConfirmPasswordResetIP); err != nil {
		return nil, err
	}
	svcReq := new(authenticatorv1.ConfirmPasswordResetRequest)
	svcReq.SetToken(req.GetToken())
	svcReq.SetNewPassword(req.GetNewPassword())

	svcResp, err := s.svcCtx.AuthenticatorClient.ConfirmPasswordReset(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	return &apiv1.ConfirmPasswordResetResponse{
		Ok: new(svcResp.GetOk()),
	}, nil
}

func (s *authenticatorServer) RequestEmailVerification(ctx context.Context, _ *apiv1.RequestEmailVerificationRequest) (*apiv1.RequestEmailVerificationResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	if err := apiratelimit.CheckIP(ctx, apiratelimit.PolicyRecoveryRequestIP); err != nil {
		return nil, err
	}

	svcReq := new(authenticatorv1.RequestEmailVerificationRequest)
	svcReq.SetUserId(auth.GetUserId())

	svcResp, err := s.svcCtx.AuthenticatorClient.RequestEmailVerification(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	return &apiv1.RequestEmailVerificationResponse{
		Ok: new(svcResp.GetOk()),
	}, nil
}

func (s *authenticatorServer) ConfirmEmailVerification(ctx context.Context, req *apiv1.ConfirmEmailVerificationRequest) (*apiv1.ConfirmEmailVerificationResponse, error) {
	svcReq := new(authenticatorv1.ConfirmEmailVerificationRequest)
	svcReq.SetToken(req.GetToken())

	svcResp, err := s.svcCtx.AuthenticatorClient.ConfirmEmailVerification(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	return &apiv1.ConfirmEmailVerificationResponse{
		Ok: new(svcResp.GetOk()),
	}, nil
}
