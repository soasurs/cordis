package server

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/password"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/store"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/token"
)

const maxNameLength = 64

func (s *authenticatorServer) Register(ctx context.Context, req *authenticatorv1.RegisterRequest) (*authenticatorv1.RegisterResponse, error) {
	name := strings.TrimSpace(req.GetName())
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if len(name) > maxNameLength {
		return nil, status.Error(codes.InvalidArgument, "name is too long")
	}
	if strings.TrimSpace(req.GetEmail()) == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}
	if !isValidEmail(req.GetEmail()) {
		return nil, status.Error(codes.InvalidArgument, "invalid email format")
	}
	if req.GetPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "password is required")
	}

	hashedPassword, err := password.Hash(req.GetPassword())
	if err != nil {
		return nil, err
	}

	createReq := new(userv1.CreateUserRequest)
	createReq.SetName(req.GetName())
	createReq.SetEmail(req.GetEmail())

	var userID int64
	createResp, err := s.svcCtx.UserClient.CreateUser(ctx, createReq)
	switch {
	case err == nil:
		userID = createResp.GetUser().GetUserId()
	case status.Code(err) == codes.AlreadyExists:
		// The user row may be a leftover from a registration that failed
		// before the credential was stored. Such an account has never been
		// able to log in and holds no data, so letting the same email claim
		// it is equivalent to an idempotent retry. CreateUserCredential's
		// insert-if-absent semantics arbitrate races: whoever lands the
		// credential first wins, everyone else keeps the AlreadyExists.
		getUserReq := new(userv1.GetUserRequest)
		getUserReq.SetEmail(req.GetEmail())
		getUserResp, getUserErr := s.svcCtx.UserClient.GetUser(ctx, getUserReq)
		if getUserErr != nil || getUserResp.GetUser().GetUserId() <= 0 {
			return nil, err
		}
		userID = getUserResp.GetUser().GetUserId()
		if _, credentialErr := s.svcCtx.Store.GetUserCredential(ctx, userID, false); credentialErr == nil {
			return nil, err
		} else if !errors.Is(credentialErr, sql.ErrNoRows) {
			return nil, credentialErr
		}
	default:
		return nil, err
	}

	now := time.Now().UnixMilli()
	if err := s.svcCtx.Store.CreateUserCredential(ctx, &model.UserCredential{
		UserID:         userID,
		HashedPassword: hashedPassword,
		CreatedAt:      now,
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, rpcerror.New(codes.AlreadyExists, rpcerror.UserDomain, rpcerror.UserEmailAlreadyExists, "email already exists")
		}
		return nil, err
	}

	result, err := s.createSession(ctx, userID, req.GetUserAgent(), req.GetIp())
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

	getUserReq := new(userv1.GetUserRequest)
	getUserReq.SetEmail(req.GetEmail())
	getUserResp, err := s.svcCtx.UserClient.GetUser(ctx, getUserReq)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			// Burn a verification anyway so unknown emails cost the same as
			// wrong passwords.
			_, _ = password.Verify(dummyPasswordHash, req.GetPassword())
			return nil, invalidCredentialsError()
		}
		return nil, err
	}
	userID := getUserResp.GetUser().GetUserId()
	if userID <= 0 {
		return nil, invalidCredentialsError()
	}

	ok, err := s.verifyUserPassword(ctx, userID, req.GetPassword())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, invalidCredentialsError()
	}

	factor, err := s.svcCtx.Store.GetTOTPFactor(ctx, userID, false)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if factor != nil {
		challenge, err := s.createTwoFactorLoginChallenge(ctx, userID, req.GetUserAgent(), req.GetIp())
		if err != nil {
			return nil, err
		}
		resp := new(authenticatorv1.LoginResponse)
		resp.SetTwoFactorChallenge(challenge)
		return resp, nil
	}

	result, err := s.createSession(ctx, userID, req.GetUserAgent(), req.GetIp())
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
	return s.createSessionWithStore(ctx, s.svcCtx.Store, userID, userAgent, ip)
}

func (s *authenticatorServer) createSessionWithStore(ctx context.Context, sessionStore store.Store, userID int64, userAgent, ip string) (*authenticatorv1.AuthenticationResult, error) {
	now := time.Now()
	sessionID := s.svcCtx.Snowflake.Generate().Int64()
	sessionExpiresAt := now.Add(s.svcCtx.Cfg.Sessions.TTL).UnixMilli()

	refreshToken, err := s.svcCtx.Tokens.IssueRefreshToken(userID, sessionID, sessionExpiresAt, now)
	if err != nil {
		return nil, err
	}

	session, err := sessionStore.CreateSession(ctx, sessionID, userID, token.Hash(refreshToken.Raw), userAgent, ip, sessionExpiresAt)
	if err != nil {
		return nil, err
	}

	accessToken, err := s.svcCtx.Tokens.IssueAccessToken(userID, session.SessionID, now)
	if err != nil {
		return nil, err
	}

	return newAuthenticationResult(userID, session.SessionID, accessToken, refreshToken, session.ExpiresAt), nil
}

func invalidTwoFactorCodeError() error {
	return rpcerror.New(codes.Unauthenticated, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidTwoFactorCode, "invalid two-factor code")
}

func twoFactorChallengeExpiredError() error {
	return rpcerror.New(codes.Unauthenticated, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorTwoFactorChallengeExpired, "two-factor challenge expired")
}

func twoFactorNotEnabledError() error {
	return rpcerror.New(codes.FailedPrecondition, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorTwoFactorNotEnabled, "two-factor authentication is not enabled")
}

func twoFactorAlreadyEnabledError() error {
	return rpcerror.New(codes.FailedPrecondition, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorTwoFactorAlreadyEnabled, "two-factor authentication is already enabled")
}

func twoFactorEnrollmentPendingError() error {
	return rpcerror.New(codes.FailedPrecondition, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorTwoFactorEnrollmentPending, "two-factor enrollment is already pending")
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
