package server

import (
	"context"
	"net"

	"connectrpc.com/connect"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/pkg/apierror"
)

func (s *authenticatorServer) Register(ctx context.Context, req *apiv1.RegisterRequest) (*apiv1.RegisterResponse, error) {
	svcReq := new(authenticatorv1.RegisterRequest)
	svcReq.SetName(req.GetName())
	svcReq.SetEmail(req.GetEmail())
	svcReq.SetPassword(req.GetPassword())
	setClientMetadata(ctx, svcReq.SetUserAgent, svcReq.SetIp)

	svcResp, err := s.svcCtx.AuthenticatorClient.Register(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	return &apiv1.RegisterResponse{
		Result: toAPIAuthenticationResult(svcResp.GetResult()),
	}, nil
}

func (s *authenticatorServer) Login(ctx context.Context, req *apiv1.LoginRequest) (*apiv1.LoginResponse, error) {
	svcReq := new(authenticatorv1.LoginRequest)
	svcReq.SetEmail(req.GetEmail())
	svcReq.SetPassword(req.GetPassword())
	setClientMetadata(ctx, svcReq.SetUserAgent, svcReq.SetIp)

	svcResp, err := s.svcCtx.AuthenticatorClient.Login(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	if svcResp.GetResult() != nil {
		return &apiv1.LoginResponse{
			Outcome: &apiv1.LoginResponse_Result{Result: toAPIAuthenticationResult(svcResp.GetResult())},
		}, nil
	}
	return &apiv1.LoginResponse{
		Outcome: &apiv1.LoginResponse_TwoFactorChallenge{TwoFactorChallenge: toAPITwoFactorLoginChallenge(svcResp.GetTwoFactorChallenge())},
	}, nil
}

func (s *authenticatorServer) CompleteTwoFactorLogin(ctx context.Context, req *apiv1.CompleteTwoFactorLoginRequest) (*apiv1.CompleteTwoFactorLoginResponse, error) {
	svcReq := new(authenticatorv1.CompleteTwoFactorLoginRequest)
	svcReq.SetChallengeToken(req.GetChallengeToken())
	svcReq.SetCode(req.GetCode())
	setClientMetadata(ctx, svcReq.SetUserAgent, svcReq.SetIp)
	svcResp, err := s.svcCtx.AuthenticatorClient.CompleteTwoFactorLogin(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.CompleteTwoFactorLoginResponse{Result: toAPIAuthenticationResult(svcResp.GetResult())}, nil
}

func (s *authenticatorServer) Refresh(ctx context.Context, req *apiv1.RefreshRequest) (*apiv1.RefreshResponse, error) {
	svcReq := new(authenticatorv1.RefreshRequest)
	svcReq.SetRefreshToken(req.GetRefreshToken())

	svcResp, err := s.svcCtx.AuthenticatorClient.Refresh(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	return &apiv1.RefreshResponse{
		Result: toAPIAuthenticationResult(svcResp.GetResult()),
	}, nil
}

func (s *authenticatorServer) Logout(ctx context.Context, req *apiv1.LogoutRequest) (*apiv1.LogoutResponse, error) {
	svcReq := new(authenticatorv1.LogoutRequest)
	svcReq.SetRefreshToken(req.GetRefreshToken())

	svcResp, err := s.svcCtx.AuthenticatorClient.Logout(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	return &apiv1.LogoutResponse{
		Ok: new(svcResp.GetOk()),
	}, nil
}

func (s *authenticatorServer) ListSessions(ctx context.Context, _ *apiv1.ListSessionsRequest) (*apiv1.ListSessionsResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}

	svcReq := new(authenticatorv1.ListSessionsRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcResp, err := s.svcCtx.AuthenticatorClient.ListSessions(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	sessions := make([]*apiv1.Session, 0, len(svcResp.GetSessions()))
	for _, session := range svcResp.GetSessions() {
		sessions = append(sessions, &apiv1.Session{
			SessionId: new(session.GetSessionId()),
			UserAgent: new(session.GetUserAgent()),
			Ip:        new(session.GetIp()),
			CreatedAt: new(session.GetCreatedAt()),
			UpdatedAt: new(session.GetUpdatedAt()),
			ExpiresAt: new(session.GetExpiresAt()),
			Current:   new(session.GetSessionId() == auth.GetSessionId()),
		})
	}
	return &apiv1.ListSessionsResponse{Sessions: sessions}, nil
}

func (s *authenticatorServer) RevokeSession(ctx context.Context, req *apiv1.RevokeSessionRequest) (*apiv1.RevokeSessionResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}

	svcReq := new(authenticatorv1.RevokeUserSessionRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetSessionId(req.GetSessionId())
	svcResp, err := s.svcCtx.AuthenticatorClient.RevokeUserSession(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.RevokeSessionResponse{Ok: new(svcResp.GetOk())}, nil
}

func (s *authenticatorServer) GetTwoFactorStatus(ctx context.Context, _ *apiv1.GetTwoFactorStatusRequest) (*apiv1.GetTwoFactorStatusResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(authenticatorv1.GetTwoFactorStatusRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcResp, err := s.svcCtx.AuthenticatorClient.GetTwoFactorStatus(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.GetTwoFactorStatusResponse{Enabled: new(svcResp.GetEnabled()), RecoveryCodesRemaining: new(svcResp.GetRecoveryCodesRemaining())}, nil
}

func (s *authenticatorServer) BeginTwoFactorEnrollment(ctx context.Context, req *apiv1.BeginTwoFactorEnrollmentRequest) (*apiv1.BeginTwoFactorEnrollmentResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(authenticatorv1.BeginTwoFactorEnrollmentRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetPassword(req.GetPassword())
	svcResp, err := s.svcCtx.AuthenticatorClient.BeginTwoFactorEnrollment(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.BeginTwoFactorEnrollmentResponse{
		EnrollmentToken: new(svcResp.GetEnrollmentToken()),
		OtpauthUri:      new(svcResp.GetOtpauthUri()),
		ManualEntryKey:  new(svcResp.GetManualEntryKey()),
		ExpiresAt:       new(svcResp.GetExpiresAt()),
	}, nil
}

func (s *authenticatorServer) ConfirmTwoFactorEnrollment(ctx context.Context, req *apiv1.ConfirmTwoFactorEnrollmentRequest) (*apiv1.ConfirmTwoFactorEnrollmentResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(authenticatorv1.ConfirmTwoFactorEnrollmentRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetCurrentSessionId(auth.GetSessionId())
	svcReq.SetEnrollmentToken(req.GetEnrollmentToken())
	svcReq.SetCode(req.GetCode())
	svcResp, err := s.svcCtx.AuthenticatorClient.ConfirmTwoFactorEnrollment(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.ConfirmTwoFactorEnrollmentResponse{RecoveryCodes: svcResp.GetRecoveryCodes()}, nil
}

func (s *authenticatorServer) DisableTwoFactor(ctx context.Context, req *apiv1.DisableTwoFactorRequest) (*apiv1.DisableTwoFactorResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(authenticatorv1.DisableTwoFactorRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetCurrentSessionId(auth.GetSessionId())
	svcReq.SetPassword(req.GetPassword())
	switch verification := req.GetVerification().(type) {
	case *apiv1.DisableTwoFactorRequest_Code:
		svcReq.SetCode(verification.Code)
	case *apiv1.DisableTwoFactorRequest_RecoveryCode:
		svcReq.SetRecoveryCode(verification.RecoveryCode)
	}
	svcResp, err := s.svcCtx.AuthenticatorClient.DisableTwoFactor(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.DisableTwoFactorResponse{Ok: new(svcResp.GetOk())}, nil
}

func (s *authenticatorServer) RegenerateTwoFactorRecoveryCodes(ctx context.Context, req *apiv1.RegenerateTwoFactorRecoveryCodesRequest) (*apiv1.RegenerateTwoFactorRecoveryCodesResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(authenticatorv1.RegenerateTwoFactorRecoveryCodesRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetCurrentSessionId(auth.GetSessionId())
	svcReq.SetPassword(req.GetPassword())
	svcReq.SetCode(req.GetCode())
	svcResp, err := s.svcCtx.AuthenticatorClient.RegenerateTwoFactorRecoveryCodes(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.RegenerateTwoFactorRecoveryCodesResponse{RecoveryCodes: svcResp.GetRecoveryCodes()}, nil
}

func setClientMetadata(ctx context.Context, setUserAgent, setIP func(string)) {
	callInfo, ok := connect.CallInfoForHandlerContext(ctx)
	if !ok {
		return
	}

	setUserAgent(callInfo.RequestHeader().Get("User-Agent"))
	setIP(clientIP(callInfo.Peer().Addr))
}

func clientIP(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err == nil {
		return host
	}
	return address
}

func toAPIAuthenticationResult(result *authenticatorv1.AuthenticationResult) *apiv1.AuthenticationResult {
	if result == nil {
		return &apiv1.AuthenticationResult{
			Ok: new(false),
		}
	}

	return &apiv1.AuthenticationResult{
		Ok:                    new(result.GetOk()),
		UserId:                new(result.GetUserId()),
		SessionId:             new(result.GetSessionId()),
		AccessToken:           new(result.GetAccessToken()),
		AccessTokenExpiresAt:  new(result.GetAccessTokenExpiresAt()),
		RefreshToken:          new(result.GetRefreshToken()),
		RefreshTokenExpiresAt: new(result.GetRefreshTokenExpiresAt()),
		SessionExpiresAt:      new(result.GetSessionExpiresAt()),
	}
}

func toAPITwoFactorLoginChallenge(challenge *authenticatorv1.TwoFactorLoginChallenge) *apiv1.TwoFactorLoginChallenge {
	if challenge == nil {
		return nil
	}
	return &apiv1.TwoFactorLoginChallenge{Token: new(challenge.GetToken()), ExpiresAt: new(challenge.GetExpiresAt())}
}
