package server

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	mailerv1 "github.com/soasurs/cordis/gen/mailer/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/mail"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/store"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/token"
)

func (s *authenticatorServer) RequestPasswordReset(ctx context.Context, req *authenticatorv1.RequestPasswordResetRequest) (*authenticatorv1.RequestPasswordResetResponse, error) {
	email := strings.TrimSpace(req.GetEmail())
	if email == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}
	if !isValidEmail(email) {
		return nil, status.Error(codes.InvalidArgument, "invalid email format")
	}

	resp := new(authenticatorv1.RequestPasswordResetResponse)
	resp.SetOk(true)

	// Throttle before the user lookup so repeated probes cost nothing and,
	// more importantly, so an attacker cannot keep replacing the victim's
	// pending token faster than the victim can use it. The throttled
	// response is indistinguishable from a successful one.
	if !s.allowRecoveryRequest(ctx, "pwreset:"+token.Hash(strings.ToLower(email))) {
		return resp, nil
	}

	getUserReq := new(userv1.GetUserRequest)
	getUserReq.SetEmail(email)
	getUserResp, err := s.svcCtx.UserClient.GetUser(ctx, getUserReq)
	if err != nil {
		// Unknown accounts report success so the endpoint cannot be used to
		// probe registered emails.
		if status.Code(err) == codes.NotFound {
			return resp, nil
		}
		return nil, err
	}
	user := getUserResp.GetUser()
	if user.GetUserId() <= 0 {
		return resp, nil
	}
	if _, err := s.svcCtx.Store.GetUserCredential(ctx, user.GetUserId(), false); errors.Is(err, sql.ErrNoRows) {
		// An account without a credential has not completed registration.
		// It must resume through Register so an invitation cannot be bypassed.
		return resp, nil
	} else if err != nil {
		return nil, err
	}

	rawToken, err := token.GenerateOpaqueToken()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if err := s.svcCtx.Store.UpsertPasswordResetToken(ctx, &model.PasswordResetToken{
		UserID:    user.GetUserId(),
		TokenHash: token.Hash(rawToken),
		CreatedAt: now.UnixMilli(),
		ExpiresAt: now.Add(s.svcCtx.Cfg.Recovery.PasswordResetTTL).UnixMilli(),
	}); err != nil {
		return nil, err
	}

	// Delivery failures stay internal: the response must not reveal whether
	// an email was actually sent.
	s.sendRecoveryMail(ctx, user.GetEmail(), mail.TemplatePasswordReset, rawToken)
	return resp, nil
}

func (s *authenticatorServer) ConfirmPasswordReset(ctx context.Context, req *authenticatorv1.ConfirmPasswordResetRequest) (*authenticatorv1.ConfirmPasswordResetResponse, error) {
	rawToken := strings.TrimSpace(req.GetToken())
	if rawToken == "" {
		return nil, status.Error(codes.InvalidArgument, "token is required")
	}
	if req.GetNewPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "new password is required")
	}

	tokenHash := token.Hash(rawToken)
	precheckNow := time.Now().UnixMilli()
	reset, err := s.svcCtx.Store.GetPasswordResetToken(ctx, tokenHash, false)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, invalidPasswordResetTokenError()
	}
	if err != nil {
		return nil, err
	}
	if reset.ConsumedAt != 0 || reset.ExpiresAt <= precheckNow {
		return nil, invalidPasswordResetTokenError()
	}

	// Hash only after the low-cost token precheck, and before taking row locks.
	hashedPassword, err := s.hashPassword(ctx, req.GetNewPassword())
	if err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	// Credentials live in this database, so consuming the token, replacing
	// the password, and revoking every session commit atomically.
	err = s.svcCtx.Store.Transact(ctx, func(tx store.Store) error {
		reset, err := tx.GetPasswordResetToken(ctx, tokenHash, true)
		if errors.Is(err, sql.ErrNoRows) {
			return invalidPasswordResetTokenError()
		}
		if err != nil {
			return err
		}
		if reset.ConsumedAt != 0 || reset.ExpiresAt <= now {
			return invalidPasswordResetTokenError()
		}
		if err := tx.UpdateUserCredential(ctx, reset.UserID, hashedPassword, now); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return invalidPasswordResetTokenError()
			}
			return err
		}
		if err := tx.ConsumePasswordResetToken(ctx, tokenHash, now); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return invalidPasswordResetTokenError()
			}
			return err
		}

		// A recovered account signs out everywhere, including whoever
		// currently holds the sessions.
		_, err = tx.RevokeOtherSessions(ctx, reset.UserID, 0)
		return err
	})
	if err != nil {
		return nil, err
	}

	resp := new(authenticatorv1.ConfirmPasswordResetResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *authenticatorServer) RequestEmailVerification(ctx context.Context, req *authenticatorv1.RequestEmailVerificationRequest) (*authenticatorv1.RequestEmailVerificationResponse, error) {
	email := strings.ToLower(strings.TrimSpace(req.GetEmail()))
	if email == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}
	if !isValidEmail(email) {
		return nil, status.Error(codes.InvalidArgument, "invalid email format")
	}

	resp := new(authenticatorv1.RequestEmailVerificationResponse)
	resp.SetOk(true)
	if !s.allowRecoveryRequest(ctx, "emailverify:"+token.Hash(email)) {
		return resp, nil
	}

	getUserReq := new(userv1.GetUserRequest)
	getUserReq.SetEmail(email)
	getUserResp, err := s.svcCtx.UserClient.GetUser(ctx, getUserReq)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return resp, nil
		}
		return nil, err
	}
	user := getUserResp.GetUser()
	if user.GetUserId() <= 0 || strings.ToLower(user.GetEmail()) != email {
		return resp, nil
	}
	if _, err := s.svcCtx.Store.GetUserCredential(ctx, user.GetUserId(), false); errors.Is(err, sql.ErrNoRows) {
		return resp, nil
	} else if err != nil {
		return nil, err
	}

	// Requesting verification for an already-verified email is a no-op.
	if user.GetEmailVerifiedAt() != 0 {
		return resp, nil
	}

	rawToken, err := token.GenerateOpaqueToken()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if err := s.svcCtx.Store.UpsertEmailVerificationToken(ctx, &model.EmailVerificationToken{
		UserID:    user.GetUserId(),
		TokenHash: token.Hash(rawToken),
		Email:     user.GetEmail(),
		CreatedAt: now.UnixMilli(),
		ExpiresAt: now.Add(s.svcCtx.Cfg.Recovery.EmailVerificationTTL).UnixMilli(),
	}); err != nil {
		return nil, err
	}

	s.sendRecoveryMail(ctx, user.GetEmail(), mail.TemplateEmailVerification, rawToken)
	return resp, nil
}

func (s *authenticatorServer) ConfirmEmailVerification(ctx context.Context, req *authenticatorv1.ConfirmEmailVerificationRequest) (*authenticatorv1.ConfirmEmailVerificationResponse, error) {
	rawToken := strings.TrimSpace(req.GetToken())
	if rawToken == "" {
		return nil, status.Error(codes.InvalidArgument, "token is required")
	}

	tokenHash := token.Hash(rawToken)
	now := time.Now().UnixMilli()
	err := s.svcCtx.Store.Transact(ctx, func(tx store.Store) error {
		verification, err := tx.GetEmailVerificationToken(ctx, tokenHash, true)
		if errors.Is(err, sql.ErrNoRows) {
			return invalidEmailVerificationTokenError()
		}
		if err != nil {
			return err
		}
		if verification.ConsumedAt != 0 || verification.ExpiresAt <= now {
			return invalidEmailVerificationTokenError()
		}
		if err := tx.ConsumeEmailVerificationToken(ctx, tokenHash, now); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return invalidEmailVerificationTokenError()
			}
			return err
		}

		markReq := new(userv1.MarkEmailVerifiedRequest)
		markReq.SetUserId(verification.UserID)
		markReq.SetEmail(verification.Email)
		markReq.SetVerifiedAt(now)
		if _, err := s.svcCtx.UserClient.MarkEmailVerified(ctx, markReq); err != nil {
			// The user replaced their email after the token was issued.
			if status.Code(err) == codes.NotFound {
				return invalidEmailVerificationTokenError()
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	resp := new(authenticatorv1.ConfirmEmailVerificationResponse)
	resp.SetOk(true)
	return resp, nil
}

// allowRecoveryRequest applies per-target throttling for recovery mail. It
// fails open: throttling protects against abuse, and a Redis outage must not
// disable account recovery.
func (s *authenticatorServer) allowRecoveryRequest(ctx context.Context, key string) bool {
	if s.svcCtx.RecoveryLimiter == nil {
		return true
	}
	allowed, err := s.svcCtx.RecoveryLimiter.Allow(ctx, key)
	if err != nil {
		logx.WithContext(ctx).Errorw("recovery limiter", logx.Field("error", err))
		return true
	}
	if !allowed {
		logx.WithContext(ctx).Infow("recovery request throttled", logx.Field("key", key))
	}
	return allowed
}

// sendRecoveryMail delivers a recovery token through the mailer service on a
// best-effort basis. Failures and a missing mailer only log: recovery
// responses must not reveal whether mail went out.
func (s *authenticatorServer) sendRecoveryMail(ctx context.Context, email, template, rawToken string) {
	if s.svcCtx.MailerClient == nil {
		logx.WithContext(ctx).Infow("mailer not configured; skipping recovery mail", logx.Field("template", template))
		return
	}
	req := new(mailerv1.SendEmailRequest)
	req.SetTo(email)
	req.SetTemplate(template)
	req.SetVariables(map[string]string{mail.VariableToken: rawToken})
	if _, err := s.svcCtx.MailerClient.SendEmail(ctx, req); err != nil {
		logx.WithContext(ctx).Errorw("send recovery mail", logx.Field("template", template), logx.Field("error", err))
	}
}

func invalidPasswordResetTokenError() error {
	return rpcerror.New(codes.InvalidArgument, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidPasswordResetToken, "invalid or expired password reset token")
}

func invalidEmailVerificationTokenError() error {
	return rpcerror.New(codes.InvalidArgument, rpcerror.AuthenticatorDomain, rpcerror.AuthenticatorInvalidEmailVerificationToken, "invalid or expired email verification token")
}
