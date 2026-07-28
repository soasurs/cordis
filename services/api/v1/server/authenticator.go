package server

import (
	"context"
	"net"

	"connectrpc.com/connect"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	"github.com/soasurs/cordis/services/api/v1/config"
	apiratelimit "github.com/soasurs/cordis/services/api/v1/ratelimit"
)

func (s *authenticatorServer) Register(ctx context.Context, req *apiv1.RegisterRequest) (*apiv1.RegisterResponse, error) {
	if err := apiratelimit.CheckIP(ctx, apiratelimit.PolicyRegisterIP); err != nil {
		return nil, err
	}
	if err := apiratelimit.CheckKey(ctx, apiratelimit.PolicyRegisterEmail, apiratelimit.EmailKey(req.GetEmail())); err != nil {
		return nil, err
	}
	svcReq := new(authenticatorv1.RegisterRequest)
	svcReq.SetName(req.GetName())
	svcReq.SetEmail(req.GetEmail())
	svcReq.SetPassword(req.GetPassword())
	svcReq.SetUsername(req.GetUsername())
	svcReq.SetRegistrationInviteCode(req.GetRegistrationInviteCode())

	svcResp, err := s.svcCtx.AuthenticatorClient.Register(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	resp := new(apiv1.RegisterResponse)
	resp.SetOk(svcResp.GetOk())
	return resp, nil
}

func (s *authenticatorServer) Login(ctx context.Context, req *apiv1.LoginRequest) (*apiv1.LoginResponse, error) {
	if req.GetTokenTransport() == apiv1.TokenTransport_TOKEN_TRANSPORT_COOKIE {
		if err := s.requireBrowserOrigin(ctx); err != nil {
			return nil, err
		}
	}
	if err := apiratelimit.CheckIP(ctx, apiratelimit.PolicyLoginIP); err != nil {
		return nil, err
	}
	if err := apiratelimit.CheckKey(ctx, apiratelimit.PolicyLoginEmail, apiratelimit.EmailKey(req.GetEmail())); err != nil {
		return nil, err
	}
	svcReq := new(authenticatorv1.LoginRequest)
	svcReq.SetEmail(req.GetEmail())
	svcReq.SetPassword(req.GetPassword())
	setClientMetadata(ctx, svcReq.SetUserAgent, svcReq.SetIp)

	svcResp, err := s.svcCtx.AuthenticatorClient.Login(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	resp := new(apiv1.LoginResponse)
	if svcResp.GetResult() != nil {
		cookieMode := req.GetTokenTransport() == apiv1.TokenTransport_TOKEN_TRANSPORT_COOKIE
		if cookieMode {
			setResponseAuthenticationCookies(ctx, s.svcCtx.Cfg.BrowserAuth, svcResp.GetResult())
		}
		resp.SetResult(toAPIAuthenticationResult(svcResp.GetResult(), !cookieMode))
	} else {
		resp.SetTwoFactorChallenge(toAPITwoFactorLoginChallenge(svcResp.GetTwoFactorChallenge()))
	}
	return resp, nil
}

func (s *authenticatorServer) CompleteTwoFactorLogin(ctx context.Context, req *apiv1.CompleteTwoFactorLoginRequest) (*apiv1.CompleteTwoFactorLoginResponse, error) {
	if req.GetTokenTransport() == apiv1.TokenTransport_TOKEN_TRANSPORT_COOKIE {
		if err := s.requireBrowserOrigin(ctx); err != nil {
			return nil, err
		}
	}
	svcReq := new(authenticatorv1.CompleteTwoFactorLoginRequest)
	svcReq.SetChallengeToken(req.GetChallengeToken())
	svcReq.SetCode(req.GetCode())
	setClientMetadata(ctx, svcReq.SetUserAgent, svcReq.SetIp)
	svcResp, err := s.svcCtx.AuthenticatorClient.CompleteTwoFactorLogin(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.CompleteTwoFactorLoginResponse)
	cookieMode := req.GetTokenTransport() == apiv1.TokenTransport_TOKEN_TRANSPORT_COOKIE
	if cookieMode {
		setResponseAuthenticationCookies(ctx, s.svcCtx.Cfg.BrowserAuth, svcResp.GetResult())
	}
	resp.SetResult(toAPIAuthenticationResult(svcResp.GetResult(), !cookieMode))
	return resp, nil
}

func (s *authenticatorServer) Refresh(ctx context.Context, req *apiv1.RefreshRequest) (*apiv1.RefreshResponse, error) {
	refreshToken := req.GetRefreshToken()
	cookieMode := refreshToken == ""
	if cookieMode {
		if err := s.requireBrowserOrigin(ctx); err != nil {
			return nil, err
		}
		refreshToken = requestCookieFromContext(ctx, s.svcCtx.Cfg.BrowserAuth.EffectiveRefreshCookieName())
	}
	svcReq := new(authenticatorv1.RefreshRequest)
	svcReq.SetRefreshToken(refreshToken)

	svcResp, err := s.svcCtx.AuthenticatorClient.Refresh(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	resp := new(apiv1.RefreshResponse)
	if cookieMode {
		setResponseAuthenticationCookies(ctx, s.svcCtx.Cfg.BrowserAuth, svcResp.GetResult())
	}
	resp.SetResult(toAPIAuthenticationResult(svcResp.GetResult(), !cookieMode))
	return resp, nil
}

func (s *authenticatorServer) Logout(ctx context.Context, req *apiv1.LogoutRequest) (*apiv1.LogoutResponse, error) {
	refreshToken := req.GetRefreshToken()
	cookieMode := refreshToken == ""
	if cookieMode {
		if err := s.requireBrowserOrigin(ctx); err != nil {
			return nil, err
		}
		refreshToken = requestCookieFromContext(ctx, s.svcCtx.Cfg.BrowserAuth.EffectiveRefreshCookieName())
	}
	svcReq := new(authenticatorv1.LogoutRequest)
	svcReq.SetRefreshToken(refreshToken)

	svcResp, err := s.svcCtx.AuthenticatorClient.Logout(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}

	resp := new(apiv1.LogoutResponse)
	resp.SetOk(svcResp.GetOk())
	if cookieMode {
		if callInfo, ok := connect.CallInfoForHandlerContext(ctx); ok {
			clearAuthenticationCookies(callInfo.ResponseHeader(), s.svcCtx.Cfg.BrowserAuth)
		}
	}
	return resp, nil
}

func (s *authenticatorServer) CreateGatewayTicket(ctx context.Context, _ *apiv1.CreateGatewayTicketRequest) (*apiv1.CreateGatewayTicketResponse, error) {
	auth, err := authenticateWithMinimumAccessTTL(ctx, s.svcCtx.AuthenticatorClient, s.svcCtx.Cfg.BrowserAuth.GatewayTicketMinimumAccessTTL)
	if err != nil {
		return nil, err
	}
	req := new(authenticatorv1.CreateGatewayTicketRequest)
	req.SetUserId(auth.GetUserId())
	req.SetSessionId(auth.GetSessionId())
	req.SetAccessTokenExpiresAt(auth.GetExpiresAt())
	result, err := s.svcCtx.AuthenticatorClient.CreateGatewayTicket(ctx, req)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.CreateGatewayTicketResponse)
	resp.SetGatewayTicket(result.GetGatewayTicket())
	resp.SetExpiresAt(result.GetExpiresAt())
	return resp, nil
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
		s := new(apiv1.Session)
		s.SetSessionId(session.GetSessionId())
		s.SetUserAgent(session.GetUserAgent())
		s.SetIp(session.GetIp())
		s.SetCreatedAt(session.GetCreatedAt())
		s.SetUpdatedAt(session.GetUpdatedAt())
		s.SetExpiresAt(session.GetExpiresAt())
		s.SetAbsoluteExpiresAt(session.GetAbsoluteExpiresAt())
		s.SetCurrent(session.GetSessionId() == auth.GetSessionId())
		sessions = append(sessions, s)
	}
	resp := new(apiv1.ListSessionsResponse)
	resp.SetSessions(sessions)
	return resp, nil
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
	resp := new(apiv1.RevokeSessionResponse)
	resp.SetOk(svcResp.GetOk())
	if req.GetSessionId() == auth.GetSessionId() {
		if callInfo, ok := connect.CallInfoForHandlerContext(ctx); ok && callInfo.RequestHeader().Get("Authorization") == "" {
			clearAuthenticationCookies(callInfo.ResponseHeader(), s.svcCtx.Cfg.BrowserAuth)
		}
	}
	return resp, nil
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
	resp := new(apiv1.GetTwoFactorStatusResponse)
	resp.SetEnabled(svcResp.GetEnabled())
	resp.SetRecoveryCodesRemaining(svcResp.GetRecoveryCodesRemaining())
	return resp, nil
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
	resp := new(apiv1.BeginTwoFactorEnrollmentResponse)
	resp.SetEnrollmentToken(svcResp.GetEnrollmentToken())
	resp.SetOtpauthUri(svcResp.GetOtpauthUri())
	resp.SetManualEntryKey(svcResp.GetManualEntryKey())
	resp.SetExpiresAt(svcResp.GetExpiresAt())
	return resp, nil
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
	resp := new(apiv1.ConfirmTwoFactorEnrollmentResponse)
	resp.SetRecoveryCodes(svcResp.GetRecoveryCodes())
	return resp, nil
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
	if req.HasCode() {
		svcReq.SetCode(req.GetCode())
	}
	if req.HasRecoveryCode() {
		svcReq.SetRecoveryCode(req.GetRecoveryCode())
	}
	svcResp, err := s.svcCtx.AuthenticatorClient.DisableTwoFactor(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.DisableTwoFactorResponse)
	resp.SetOk(svcResp.GetOk())
	return resp, nil
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
	resp := new(apiv1.RegenerateTwoFactorRecoveryCodesResponse)
	resp.SetRecoveryCodes(svcResp.GetRecoveryCodes())
	return resp, nil
}

func setClientMetadata(ctx context.Context, setUserAgent, setIP func(string)) {
	callInfo, ok := connect.CallInfoForHandlerContext(ctx)
	if !ok {
		return
	}

	setUserAgent(callInfo.RequestHeader().Get("User-Agent"))
	if trustedIP, ok := apiratelimit.ClientIP(ctx); ok {
		setIP(trustedIP)
		return
	}
	setIP(clientIP(callInfo.Peer().Addr))
}

func clientIP(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err == nil {
		return host
	}
	return address
}

func toAPIAuthenticationResult(result *authenticatorv1.AuthenticationResult, includeTokens bool) *apiv1.AuthenticationResult {
	resp := new(apiv1.AuthenticationResult)
	if result == nil {
		resp.SetOk(false)
		return resp
	}

	resp.SetOk(result.GetOk())
	resp.SetUserId(result.GetUserId())
	resp.SetSessionId(result.GetSessionId())
	if includeTokens {
		resp.SetAccessToken(result.GetAccessToken())
		resp.SetRefreshToken(result.GetRefreshToken())
	}
	resp.SetAccessTokenExpiresAt(result.GetAccessTokenExpiresAt())
	resp.SetRefreshTokenExpiresAt(result.GetRefreshTokenExpiresAt())
	resp.SetSessionExpiresAt(result.GetSessionExpiresAt())
	resp.SetAbsoluteSessionExpiresAt(result.GetAbsoluteSessionExpiresAt())
	return resp
}

func (s *authenticatorServer) requireBrowserOrigin(ctx context.Context) error {
	callInfo, ok := connect.CallInfoForHandlerContext(ctx)
	if !ok || !allowedBrowserOrigin(callInfo.RequestHeader().Get("Origin"), s.svcCtx.Cfg.BrowserAuth.AllowedOrigins) {
		return invalidAccessTokenError()
	}
	return nil
}

func requestCookieFromContext(ctx context.Context, name string) string {
	callInfo, ok := connect.CallInfoForHandlerContext(ctx)
	if !ok {
		return ""
	}
	return requestCookie(callInfo.RequestHeader(), name)
}

func setResponseAuthenticationCookies(ctx context.Context, cfg config.BrowserAuthConfig, result *authenticatorv1.AuthenticationResult) {
	if callInfo, ok := connect.CallInfoForHandlerContext(ctx); ok {
		setAuthenticationCookies(callInfo.ResponseHeader(), cfg, result)
	}
}

func toAPITwoFactorLoginChallenge(challenge *authenticatorv1.TwoFactorLoginChallenge) *apiv1.TwoFactorLoginChallenge {
	if challenge == nil {
		return nil
	}
	resp := new(apiv1.TwoFactorLoginChallenge)
	resp.SetToken(challenge.GetToken())
	resp.SetExpiresAt(challenge.GetExpiresAt())
	return resp
}
