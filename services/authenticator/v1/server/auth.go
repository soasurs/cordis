package server

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"strings"
	"time"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/token"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *authenticatorServer) Register(ctx context.Context, req *authenticatorv1.RegisterRequest) (*authenticatorv1.RegisterResponse, error) {
	if strings.TrimSpace(req.GetName()) == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if strings.TrimSpace(req.GetEmail()) == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}
	if req.GetPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "password is required")
	}

	createReq := new(userv1.CreateUserRequest)
	createReq.SetName(req.GetName())
	createReq.SetEmail(req.GetEmail())
	createReq.SetPassword(req.GetPassword())

	createResp, err := s.svcCtx.UserClient.CreateUser(ctx, createReq)
	if err != nil {
		return nil, err
	}

	result, err := s.createSession(ctx, createResp.GetUser().GetUserId(), req.GetUserAgent(), req.GetIp())
	if err != nil {
		return nil, err
	}

	resp := new(authenticatorv1.RegisterResponse)
	resp.SetResult(result)
	return resp, nil
}

func (s *authenticatorServer) Login(ctx context.Context, req *authenticatorv1.LoginRequest) (*authenticatorv1.LoginResponse, error) {
	if strings.TrimSpace(req.GetEmail()) == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}
	if req.GetPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "password is required")
	}

	verifyReq := new(userv1.VerifyPasswordRequest)
	verifyReq.SetEmail(req.GetEmail())
	verifyReq.SetPassword(req.GetPassword())

	verifyResp, err := s.svcCtx.UserClient.VerifyPassword(ctx, verifyReq)
	if err != nil {
		return nil, err
	}
	if !verifyResp.GetOk() {
		return nil, invalidCredentialsError()
	}

	result, err := s.createSession(ctx, verifyResp.GetUserId(), req.GetUserAgent(), req.GetIp())
	if err != nil {
		return nil, err
	}

	resp := new(authenticatorv1.LoginResponse)
	resp.SetResult(result)
	return resp, nil
}

func (s *authenticatorServer) Refresh(ctx context.Context, req *authenticatorv1.RefreshRequest) (*authenticatorv1.RefreshResponse, error) {
	if req.GetRefreshToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "refresh token is required")
	}

	_, session, err := s.getSessionWithRefreshToken(ctx, req.GetRefreshToken())
	if err != nil {
		return nil, err
	}

	now := time.Now()
	newRefreshToken, err := s.svcCtx.Tokens.IssueRefreshToken(session.UserID, session.SessionID, session.ExpiresAt, now)
	if err != nil {
		return nil, err
	}
	accessToken, err := s.svcCtx.Tokens.IssueAccessToken(session.UserID, session.SessionID, now)
	if err != nil {
		return nil, err
	}

	oldRefreshTokenHash := token.Hash(req.GetRefreshToken())
	if err := s.svcCtx.Store.RotateRefreshToken(ctx, session.SessionID, oldRefreshTokenHash, token.Hash(newRefreshToken.Raw)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, invalidRefreshTokenError()
		}
		return nil, err
	}

	result := newAuthenticationResult(session.UserID, session.SessionID, accessToken, newRefreshToken, session.ExpiresAt)
	resp := new(authenticatorv1.RefreshResponse)
	resp.SetResult(result)
	return resp, nil
}

func (s *authenticatorServer) Logout(ctx context.Context, req *authenticatorv1.LogoutRequest) (*authenticatorv1.LogoutResponse, error) {
	if req.GetRefreshToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "refresh token is required")
	}

	_, session, err := s.getSessionWithRefreshToken(ctx, req.GetRefreshToken())
	if err != nil {
		return nil, err
	}

	if err := s.svcCtx.Store.RevokeSession(ctx, session.SessionID); err != nil {
		return nil, err
	}

	resp := new(authenticatorv1.LogoutResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *authenticatorServer) VerifyAccessToken(ctx context.Context, req *authenticatorv1.VerifyAccessTokenRequest) (*authenticatorv1.VerifyAccessTokenResponse, error) {
	accessToken, err := s.svcCtx.Tokens.ParseAccessToken(req.GetAccessToken())
	if err != nil {
		return nil, invalidAccessTokenError()
	}

	session, err := s.svcCtx.Store.GetSession(ctx, accessToken.SessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, invalidAccessTokenError()
		}
		return nil, err
	}
	if err := checkSession(session, time.Now().UnixMilli()); err != nil {
		return nil, err
	}
	if session.UserID != accessToken.UserID {
		return nil, invalidAccessTokenError()
	}

	resp := new(authenticatorv1.VerifyAccessTokenResponse)
	resp.SetOk(true)
	resp.SetUserId(accessToken.UserID)
	resp.SetSessionId(accessToken.SessionID)
	resp.SetExpiresAt(accessToken.ExpiresAt)
	return resp, nil
}

func (s *authenticatorServer) createSession(ctx context.Context, userID int64, userAgent, ip string) (*authenticatorv1.AuthenticationResult, error) {
	now := time.Now()
	sessionID := s.svcCtx.Snowflake.Generate().Int64()
	sessionExpiresAt := now.Add(s.svcCtx.Cfg.Sessions.TTL).UnixMilli()

	refreshToken, err := s.svcCtx.Tokens.IssueRefreshToken(userID, sessionID, sessionExpiresAt, now)
	if err != nil {
		return nil, err
	}

	session, err := s.svcCtx.Store.CreateSession(ctx, sessionID, userID, token.Hash(refreshToken.Raw), userAgent, ip, sessionExpiresAt)
	if err != nil {
		return nil, err
	}

	accessToken, err := s.svcCtx.Tokens.IssueAccessToken(userID, session.SessionID, now)
	if err != nil {
		return nil, err
	}

	return newAuthenticationResult(userID, session.SessionID, accessToken, refreshToken, session.ExpiresAt), nil
}

func (s *authenticatorServer) getSessionWithRefreshToken(ctx context.Context, rawRefreshToken string) (token.Token, *model.Session, error) {
	refreshToken, err := s.svcCtx.Tokens.ParseRefreshToken(rawRefreshToken)
	if err != nil {
		return token.Token{}, nil, invalidRefreshTokenError()
	}

	session, err := s.svcCtx.Store.GetSession(ctx, refreshToken.SessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return token.Token{}, nil, invalidRefreshTokenError()
		}
		return token.Token{}, nil, err
	}

	if err := checkSession(session, time.Now().UnixMilli()); err != nil {
		return token.Token{}, nil, err
	}
	if session.UserID != refreshToken.UserID ||
		subtle.ConstantTimeCompare([]byte(session.RefreshTokenHash), []byte(token.Hash(rawRefreshToken))) != 1 {
		return token.Token{}, nil, invalidRefreshTokenError()
	}

	return refreshToken, session, nil
}

func checkSession(session *model.Session, now int64) error {
	if session.RevokedAt != 0 {
		return rpcerror.New(codes.Unauthenticated, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorSessionRevoked, "session revoked")
	}
	if session.ExpiresAt <= now {
		return rpcerror.New(codes.Unauthenticated, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorSessionExpired, "session expired")
	}
	return nil
}

func invalidCredentialsError() error {
	return rpcerror.New(codes.Unauthenticated, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidCredentials, "invalid credentials")
}

func invalidAccessTokenError() error {
	return rpcerror.New(codes.Unauthenticated, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidAccessToken, "invalid access token")
}

func invalidRefreshTokenError() error {
	return rpcerror.New(codes.Unauthenticated, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidRefreshToken, "invalid refresh token")
}

func newAuthenticationResult(userID, sessionID int64, accessToken, refreshToken token.Token, sessionExpiresAt int64) *authenticatorv1.AuthenticationResult {
	resp := new(authenticatorv1.AuthenticationResult)
	resp.SetOk(true)
	resp.SetUserId(userID)
	resp.SetSessionId(sessionID)
	resp.SetAccessToken(accessToken.Raw)
	resp.SetAccessTokenExpiresAt(accessToken.ExpiresAt)
	resp.SetRefreshToken(refreshToken.Raw)
	resp.SetRefreshTokenExpiresAt(refreshToken.ExpiresAt)
	resp.SetSessionExpiresAt(sessionExpiresAt)
	return resp
}
