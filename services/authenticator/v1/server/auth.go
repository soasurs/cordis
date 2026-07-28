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
	"github.com/soasurs/cordis/pkg/mail"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/authenticator/v1/config"
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
	if strings.TrimSpace(req.GetUsername()) == "" {
		return nil, status.Error(codes.InvalidArgument, "username is required")
	}

	email := strings.ToLower(strings.TrimSpace(req.GetEmail()))
	invite, err := s.reserveRegistrationInvite(ctx, req.GetRegistrationInviteCode(), email)
	if err != nil {
		return nil, err
	}

	hashedPassword, err := s.hashPassword(ctx, req.GetPassword())
	if err != nil {
		return nil, err
	}
	rawVerificationToken, err := token.GenerateOpaqueToken()
	if err != nil {
		return nil, err
	}

	createReq := new(userv1.CreateUserRequest)
	createReq.SetName(name)
	createReq.SetEmail(email)
	createReq.SetUsername(req.GetUsername())

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
		getUserReq.SetEmail(email)
		getUserResp, getUserErr := s.svcCtx.UserClient.GetUser(ctx, getUserReq)
		if getUserErr != nil || getUserResp.GetUser().GetUserId() <= 0 {
			return nil, err
		}
		userID = getUserResp.GetUser().GetUserId()
		if _, credentialErr := s.svcCtx.Store.GetUserCredential(ctx, userID, false); credentialErr == nil {
			s.releaseRegistrationInvite(ctx, invite, email)
			return nil, err
		} else if !errors.Is(credentialErr, sql.ErrNoRows) {
			return nil, credentialErr
		}
	default:
		return nil, err
	}

	now := time.Now().UnixMilli()
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		if err := txStore.CreateUserCredential(ctx, &model.UserCredential{
			UserID:         userID,
			HashedPassword: hashedPassword,
			CreatedAt:      now,
		}); err != nil {
			return err
		}
		if invite != nil {
			if err := txStore.RedeemRegistrationInvite(ctx, invite.ID, email, userID, now); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return invalidRegistrationInviteError()
				}
				return err
			}
		}
		return txStore.UpsertEmailVerificationToken(ctx, &model.EmailVerificationToken{
			UserID:    userID,
			TokenHash: token.Hash(rawVerificationToken),
			Email:     email,
			CreatedAt: now,
			ExpiresAt: time.UnixMilli(now).Add(s.svcCtx.Cfg.Recovery.EmailVerificationTTL).UnixMilli(),
		})
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, rpcerror.New(codes.AlreadyExists, rpcerror.UserDomain, rpcerror.UserEmailAlreadyExists, "email already exists")
		}
		return nil, err
	}

	s.sendRecoveryMail(ctx, email, mail.TemplateEmailVerification, rawVerificationToken)
	resp := new(authenticatorv1.RegisterResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *authenticatorServer) reserveRegistrationInvite(
	ctx context.Context,
	rawCode, email string,
) (*model.RegistrationInvite, error) {
	switch s.svcCtx.Cfg.Registration.EffectiveMode() {
	case config.RegistrationModeOpen:
		return nil, nil
	case config.RegistrationModeClosed:
		return nil, registrationClosedError()
	case config.RegistrationModeInviteOnly:
	default:
		return nil, registrationClosedError()
	}

	code := strings.TrimSpace(rawCode)
	if code == "" {
		return nil, invalidRegistrationInviteError()
	}
	now := time.Now()
	invite, err := s.svcCtx.Store.ReserveRegistrationInvite(
		ctx,
		token.Hash(code),
		email,
		now.UnixMilli(),
		now.Add(s.svcCtx.Cfg.Registration.EffectiveReservationTTL()).UnixMilli(),
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, invalidRegistrationInviteError()
	}
	if err != nil {
		return nil, err
	}
	return invite, nil
}

func (s *authenticatorServer) releaseRegistrationInvite(
	ctx context.Context,
	invite *model.RegistrationInvite,
	email string,
) {
	if invite == nil {
		return
	}
	_ = s.svcCtx.Store.ReleaseRegistrationInvite(ctx, invite.ID, email)
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
			if _, verifyErr := s.verifyPassword(ctx, dummyPasswordHash, req.GetPassword()); verifyErr != nil {
				return nil, verifyErr
			}
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
	if getUserResp.GetUser().GetEmailVerifiedAt() == 0 {
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

	result, err := s.rotateRefreshToken(ctx, req.GetRefreshToken())
	if err != nil {
		return nil, err
	}
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

	return s.verifyParsedAccessToken(ctx, accessToken)
}

func (s *authenticatorServer) createSession(ctx context.Context, userID int64, userAgent, ip string) (*authenticatorv1.AuthenticationResult, error) {
	return s.createSessionWithStore(ctx, s.svcCtx.Store, userID, userAgent, ip)
}

func (s *authenticatorServer) createSessionWithStore(ctx context.Context, sessionStore store.Store, userID int64, userAgent, ip string) (*authenticatorv1.AuthenticationResult, error) {
	now := time.Now()
	sessionID := s.svcCtx.Snowflake.Generate().Int64()
	sessionExpiresAt := now.Add(s.svcCtx.Cfg.Sessions.IdleTTL).UnixMilli()
	absoluteExpiresAt := now.Add(s.svcCtx.Cfg.Sessions.AbsoluteTTL).UnixMilli()

	refreshToken, err := s.svcCtx.Tokens.IssueRefreshToken(userID, sessionID, sessionExpiresAt, now)
	if err != nil {
		return nil, err
	}

	session, err := sessionStore.CreateSession(ctx, store.CreateSessionParams{
		SessionID: sessionID, UserID: userID, RefreshTokenHash: token.Hash(refreshToken.Raw),
		RefreshTokenID: refreshToken.ID, RefreshTokenIssuedAt: refreshToken.IssuedAt,
		RefreshTokenExpiresAt: refreshToken.ExpiresAt, UserAgent: userAgent, IP: ip,
		ExpiresAt: sessionExpiresAt, AbsoluteExpiresAt: absoluteExpiresAt,
	})
	if err != nil {
		return nil, err
	}

	accessToken, err := s.svcCtx.Tokens.IssueAccessToken(userID, session.SessionID, now)
	if err != nil {
		return nil, err
	}

	return newAuthenticationResult(userID, session.SessionID, accessToken, refreshToken, session.ExpiresAt, session.AbsoluteExpiresAt), nil
}

func (s *authenticatorServer) rotateRefreshToken(ctx context.Context, raw string) (*authenticatorv1.AuthenticationResult, error) {
	refreshToken, err := s.svcCtx.Tokens.ParseRefreshToken(raw)
	if err != nil {
		return nil, invalidRefreshTokenError()
	}
	session, err := s.svcCtx.Store.GetSession(ctx, refreshToken.SessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, invalidRefreshTokenError()
		}
		return nil, err
	}
	now := time.Now()
	if err := checkSession(session, now.UnixMilli()); err != nil {
		return nil, err
	}
	if session.UserID != refreshToken.UserID {
		return nil, invalidRefreshTokenError()
	}
	presentedHash := token.Hash(raw)
	if subtle.ConstantTimeCompare([]byte(session.RefreshTokenHash), []byte(presentedHash)) != 1 {
		return s.replayRefreshToken(ctx, session, presentedHash, now)
	}

	expiresAt := min(now.Add(s.svcCtx.Cfg.Sessions.IdleTTL).UnixMilli(), session.AbsoluteExpiresAt)
	newRefreshToken, err := s.svcCtx.Tokens.IssueRefreshToken(session.UserID, session.SessionID, expiresAt, now)
	if err != nil {
		return nil, err
	}
	if err := s.svcCtx.Store.RotateRefreshToken(ctx, store.RotateRefreshTokenParams{
		SessionID: session.SessionID, OldRefreshTokenHash: presentedHash,
		NewRefreshTokenHash: token.Hash(newRefreshToken.Raw), NewRefreshTokenID: newRefreshToken.ID,
		NewRefreshTokenIssuedAt: newRefreshToken.IssuedAt, NewRefreshTokenExpiresAt: newRefreshToken.ExpiresAt,
		ExpiresAt: expiresAt, PreviousRefreshTokenValidUntil: now.Add(s.svcCtx.Cfg.Sessions.RotationGrace).UnixMilli(),
	}); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		latest, loadErr := s.svcCtx.Store.GetSession(ctx, session.SessionID)
		if loadErr != nil {
			return nil, loadErr
		}
		return s.replayRefreshToken(ctx, latest, presentedHash, now)
	}
	accessToken, err := s.svcCtx.Tokens.IssueAccessToken(session.UserID, session.SessionID, now)
	if err != nil {
		return nil, err
	}
	return newAuthenticationResult(session.UserID, session.SessionID, accessToken, newRefreshToken, expiresAt, session.AbsoluteExpiresAt), nil
}

func (s *authenticatorServer) replayRefreshToken(ctx context.Context, session *model.Session, presentedHash string, now time.Time) (*authenticatorv1.AuthenticationResult, error) {
	if err := checkSession(session, now.UnixMilli()); err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(session.PreviousRefreshTokenHash), []byte(presentedHash)) != 1 ||
		session.PreviousRefreshTokenValidUntil < now.UnixMilli() {
		_ = s.svcCtx.Store.RevokeSession(ctx, session.SessionID)
		return nil, invalidRefreshTokenError()
	}
	refreshToken, err := s.svcCtx.Tokens.ReissueRefreshToken(session.UserID, session.SessionID,
		session.RefreshTokenID, session.RefreshTokenIssuedAt, session.RefreshTokenExpiresAt)
	if err != nil || token.Hash(refreshToken.Raw) != session.RefreshTokenHash {
		return nil, invalidRefreshTokenError()
	}
	accessToken, err := s.svcCtx.Tokens.IssueAccessToken(session.UserID, session.SessionID, now)
	if err != nil {
		return nil, err
	}
	return newAuthenticationResult(session.UserID, session.SessionID, accessToken, refreshToken,
		session.ExpiresAt, session.AbsoluteExpiresAt), nil
}

func invalidTwoFactorCodeError() error {
	return rpcerror.New(codes.Unauthenticated, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidTwoFactorCode, "invalid two-factor code")
}

func invalidRegistrationInviteError() error {
	return rpcerror.New(codes.InvalidArgument, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidRegistrationInvite, "invalid or unavailable registration invite")
}

func registrationClosedError() error {
	return rpcerror.New(codes.FailedPrecondition, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorRegistrationClosed, "registration is closed")
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
	hash := token.Hash(rawRefreshToken)
	current := subtle.ConstantTimeCompare([]byte(session.RefreshTokenHash), []byte(hash)) == 1
	previous := subtle.ConstantTimeCompare([]byte(session.PreviousRefreshTokenHash), []byte(hash)) == 1 &&
		session.PreviousRefreshTokenValidUntil >= time.Now().UnixMilli()
	if session.UserID != refreshToken.UserID || (!current && !previous) {
		return token.Token{}, nil, invalidRefreshTokenError()
	}

	return refreshToken, session, nil
}

func checkSession(session *model.Session, now int64) error {
	if session.RevokedAt != 0 {
		return rpcerror.New(codes.Unauthenticated, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorSessionRevoked, "session revoked")
	}
	if session.ExpiresAt <= now || (session.AbsoluteExpiresAt != 0 && session.AbsoluteExpiresAt <= now) {
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

func newAuthenticationResult(userID, sessionID int64, accessToken, refreshToken token.Token, sessionExpiresAt, absoluteSessionExpiresAt int64) *authenticatorv1.AuthenticationResult {
	resp := new(authenticatorv1.AuthenticationResult)
	resp.SetOk(true)
	resp.SetUserId(userID)
	resp.SetSessionId(sessionID)
	resp.SetAccessToken(accessToken.Raw)
	resp.SetAccessTokenExpiresAt(accessToken.ExpiresAt)
	resp.SetRefreshToken(refreshToken.Raw)
	resp.SetRefreshTokenExpiresAt(refreshToken.ExpiresAt)
	resp.SetSessionExpiresAt(sessionExpiresAt)
	resp.SetAbsoluteSessionExpiresAt(absoluteSessionExpiresAt)
	return resp
}
