package server

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/store"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/token"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/twofactor"
)

func (s *authenticatorServer) CompleteTwoFactorLogin(ctx context.Context, req *authenticatorv1.CompleteTwoFactorLoginRequest) (*authenticatorv1.CompleteTwoFactorLoginResponse, error) {
	if strings.TrimSpace(req.GetChallengeToken()) == "" {
		return nil, status.Error(codes.InvalidArgument, "two-factor challenge token is required")
	}
	if strings.TrimSpace(req.GetCode()) == "" {
		return nil, status.Error(codes.InvalidArgument, "two-factor code is required")
	}

	challengeHash := token.Hash(req.GetChallengeToken())
	var result *authenticatorv1.AuthenticationResult
	var outcomeErr error
	err := s.svcCtx.Store.Transact(ctx, func(tx store.Store) error {
		challenge, err := tx.GetTwoFactorLoginChallenge(ctx, challengeHash, true)
		if errors.Is(err, sql.ErrNoRows) {
			outcomeErr = twoFactorChallengeExpiredError()
			return nil
		}
		if err != nil {
			return err
		}
		now := time.Now()
		if challenge.ConsumedAt != 0 || challenge.ExpiresAt <= now.UnixMilli() || challenge.Attempts >= s.svcCtx.Cfg.TwoFactor.MaxAttempts {
			outcomeErr = twoFactorChallengeExpiredError()
			return nil
		}

		factor, err := tx.GetTOTPFactor(ctx, challenge.UserID, true)
		if errors.Is(err, sql.ErrNoRows) {
			outcomeErr = twoFactorChallengeExpiredError()
			return nil
		}
		if err != nil {
			return err
		}
		counter, err := s.verifyTOTPCode(factor, req.GetCode(), now)
		if errors.Is(err, twofactor.ErrInvalidCode) {
			err = tx.ConsumeRecoveryCode(ctx, factor.UserID, token.Hash(twofactor.NormalizeRecoveryCode(req.GetCode())))
			if errors.Is(err, sql.ErrNoRows) {
				if err := tx.IncrementTwoFactorLoginChallengeAttempts(ctx, challengeHash); err != nil {
					return err
				}
				if challenge.Attempts+1 >= s.svcCtx.Cfg.TwoFactor.MaxAttempts {
					if err := tx.ConsumeTwoFactorLoginChallenge(ctx, challengeHash); err != nil {
						return err
					}
				}
				outcomeErr = invalidTwoFactorCodeError()
				return nil
			}
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if err := tx.UpdateTOTPLastUsedCounter(ctx, factor.UserID, counter); err != nil {
			return err
		}
		if err := tx.ConsumeTwoFactorLoginChallenge(ctx, challengeHash); err != nil {
			return err
		}

		result, err = s.createSessionWithStore(ctx, tx, challenge.UserID, challenge.UserAgent, challenge.IP)
		return err
	})
	if err != nil {
		return nil, err
	}
	if outcomeErr != nil {
		return nil, outcomeErr
	}

	resp := new(authenticatorv1.CompleteTwoFactorLoginResponse)
	resp.SetResult(result)
	return resp, nil
}

func (s *authenticatorServer) GetTwoFactorStatus(ctx context.Context, req *authenticatorv1.GetTwoFactorStatusRequest) (*authenticatorv1.GetTwoFactorStatusResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "user id is required")
	}
	factor, err := s.svcCtx.Store.GetTOTPFactor(ctx, req.GetUserId(), false)
	if errors.Is(err, sql.ErrNoRows) {
		return new(authenticatorv1.GetTwoFactorStatusResponse), nil
	}
	if err != nil {
		return nil, err
	}
	remaining, err := s.svcCtx.Store.CountUnusedRecoveryCodes(ctx, factor.UserID)
	if err != nil {
		return nil, err
	}
	resp := new(authenticatorv1.GetTwoFactorStatusResponse)
	resp.SetEnabled(true)
	resp.SetRecoveryCodesRemaining(int32(remaining))
	return resp, nil
}

func (s *authenticatorServer) BeginTwoFactorEnrollment(ctx context.Context, req *authenticatorv1.BeginTwoFactorEnrollmentRequest) (*authenticatorv1.BeginTwoFactorEnrollmentResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "user id is required")
	}
	if req.GetPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "password is required")
	}
	if _, err := s.svcCtx.Store.GetTOTPFactor(ctx, req.GetUserId(), false); err == nil {
		return nil, twoFactorAlreadyEnabledError()
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	email, err := s.verifyCurrentUserPassword(ctx, req.GetUserId(), req.GetPassword())
	if err != nil {
		return nil, err
	}
	secret, manualEntryKey, err := twofactor.GenerateSecret()
	if err != nil {
		return nil, err
	}
	defer zeroBytes(secret)
	ciphertext, err := s.svcCtx.TwoFactor.Encrypt(req.GetUserId(), secret)
	if err != nil {
		return nil, err
	}
	enrollmentToken, err := token.GenerateOpaqueToken()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	expiresAt := now.Add(s.svcCtx.Cfg.TwoFactor.EnrollmentTTL).UnixMilli()
	if err := s.svcCtx.Store.CreateTOTPEnrollment(ctx, &model.TOTPEnrollment{
		UserID: req.GetUserId(), TokenHash: token.Hash(enrollmentToken), SecretCiphertext: ciphertext.Data,
		EncryptionKeyID: ciphertext.KeyID, CreatedAt: now.UnixMilli(), ExpiresAt: expiresAt,
	}); errors.Is(err, sql.ErrNoRows) {
		return nil, twoFactorEnrollmentPendingError()
	} else if err != nil {
		return nil, err
	}

	resp := new(authenticatorv1.BeginTwoFactorEnrollmentResponse)
	resp.SetEnrollmentToken(enrollmentToken)
	resp.SetManualEntryKey(manualEntryKey)
	resp.SetOtpauthUri(twofactor.OTPAuthURI(s.svcCtx.Cfg.TwoFactor.Issuer, email, manualEntryKey))
	resp.SetExpiresAt(expiresAt)
	return resp, nil
}

func (s *authenticatorServer) ConfirmTwoFactorEnrollment(ctx context.Context, req *authenticatorv1.ConfirmTwoFactorEnrollmentRequest) (*authenticatorv1.ConfirmTwoFactorEnrollmentResponse, error) {
	if req.GetUserId() <= 0 || req.GetCurrentSessionId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "authenticated user and session are required")
	}
	if strings.TrimSpace(req.GetEnrollmentToken()) == "" || strings.TrimSpace(req.GetCode()) == "" {
		return nil, status.Error(codes.InvalidArgument, "enrollment token and two-factor code are required")
	}

	codes, hashes, err := s.generateRecoveryCodes()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var outcomeErr error
	err = s.svcCtx.Store.Transact(ctx, func(tx store.Store) error {
		enrollment, err := tx.GetTOTPEnrollment(ctx, req.GetUserId(), token.Hash(req.GetEnrollmentToken()), true)
		if errors.Is(err, sql.ErrNoRows) {
			outcomeErr = twoFactorChallengeExpiredError()
			return nil
		}
		if err != nil {
			return err
		}
		if enrollment.ExpiresAt <= now.UnixMilli() {
			if err := tx.DeleteTOTPEnrollment(ctx, enrollment.UserID, enrollment.TokenHash); err != nil {
				return err
			}
			outcomeErr = twoFactorChallengeExpiredError()
			return nil
		}
		secret, err := s.svcCtx.TwoFactor.Decrypt(enrollment.UserID, twofactor.Ciphertext{KeyID: enrollment.EncryptionKeyID, Data: enrollment.SecretCiphertext})
		if err != nil {
			return err
		}
		defer zeroBytes(secret)
		counter, err := twofactor.VerifyCode(secret, req.GetCode(), now, -1)
		if errors.Is(err, twofactor.ErrInvalidCode) {
			outcomeErr = invalidTwoFactorCodeError()
			return nil
		}
		if err != nil {
			return err
		}
		if err := tx.UpsertTOTPFactor(ctx, &model.TOTPFactor{UserID: enrollment.UserID, SecretCiphertext: enrollment.SecretCiphertext, EncryptionKeyID: enrollment.EncryptionKeyID, LastUsedCounter: counter, EnabledAt: now.UnixMilli(), CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli()}); err != nil {
			return err
		}
		if err := tx.ReplaceRecoveryCodes(ctx, enrollment.UserID, hashes); err != nil {
			return err
		}
		if err := tx.DeleteTOTPEnrollment(ctx, enrollment.UserID, enrollment.TokenHash); err != nil {
			return err
		}
		_, err = tx.RevokeOtherSessions(ctx, enrollment.UserID, req.GetCurrentSessionId())
		return err
	})
	if err != nil {
		return nil, err
	}
	if outcomeErr != nil {
		return nil, outcomeErr
	}
	resp := new(authenticatorv1.ConfirmTwoFactorEnrollmentResponse)
	resp.SetRecoveryCodes(codes)
	return resp, nil
}

func (s *authenticatorServer) DisableTwoFactor(ctx context.Context, req *authenticatorv1.DisableTwoFactorRequest) (*authenticatorv1.DisableTwoFactorResponse, error) {
	if req.GetUserId() <= 0 || req.GetCurrentSessionId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "authenticated user and session are required")
	}
	if req.GetPassword() == "" || !req.HasVerification() {
		return nil, status.Error(codes.InvalidArgument, "password and two-factor verification are required")
	}
	if _, err := s.verifyCurrentUserPassword(ctx, req.GetUserId(), req.GetPassword()); err != nil {
		return nil, err
	}

	var outcomeErr error
	err := s.svcCtx.Store.Transact(ctx, func(tx store.Store) error {
		factor, err := tx.GetTOTPFactor(ctx, req.GetUserId(), true)
		if errors.Is(err, sql.ErrNoRows) {
			outcomeErr = twoFactorNotEnabledError()
			return nil
		}
		if err != nil {
			return err
		}
		if req.HasCode() {
			counter, err := s.verifyTOTPCode(factor, req.GetCode(), time.Now())
			if errors.Is(err, twofactor.ErrInvalidCode) {
				outcomeErr = invalidTwoFactorCodeError()
				return nil
			}
			if err != nil {
				return err
			}
			if err := tx.UpdateTOTPLastUsedCounter(ctx, factor.UserID, counter); err != nil {
				return err
			}
		} else if err := tx.ConsumeRecoveryCode(ctx, factor.UserID, token.Hash(twofactor.NormalizeRecoveryCode(req.GetRecoveryCode()))); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				outcomeErr = invalidTwoFactorCodeError()
				return nil
			}
			return err
		}
		if err := tx.DeleteTOTPFactor(ctx, factor.UserID); err != nil {
			return err
		}
		if err := tx.ReplaceRecoveryCodes(ctx, factor.UserID, nil); err != nil {
			return err
		}
		_, err = tx.RevokeOtherSessions(ctx, factor.UserID, req.GetCurrentSessionId())
		return err
	})
	if err != nil {
		return nil, err
	}
	if outcomeErr != nil {
		return nil, outcomeErr
	}
	resp := new(authenticatorv1.DisableTwoFactorResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *authenticatorServer) RegenerateTwoFactorRecoveryCodes(ctx context.Context, req *authenticatorv1.RegenerateTwoFactorRecoveryCodesRequest) (*authenticatorv1.RegenerateTwoFactorRecoveryCodesResponse, error) {
	if req.GetUserId() <= 0 || req.GetCurrentSessionId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "authenticated user and session are required")
	}
	if req.GetPassword() == "" || strings.TrimSpace(req.GetCode()) == "" {
		return nil, status.Error(codes.InvalidArgument, "password and two-factor code are required")
	}
	if _, err := s.verifyCurrentUserPassword(ctx, req.GetUserId(), req.GetPassword()); err != nil {
		return nil, err
	}
	codes, hashes, err := s.generateRecoveryCodes()
	if err != nil {
		return nil, err
	}
	var outcomeErr error
	err = s.svcCtx.Store.Transact(ctx, func(tx store.Store) error {
		factor, err := tx.GetTOTPFactor(ctx, req.GetUserId(), true)
		if errors.Is(err, sql.ErrNoRows) {
			outcomeErr = twoFactorNotEnabledError()
			return nil
		}
		if err != nil {
			return err
		}
		counter, err := s.verifyTOTPCode(factor, req.GetCode(), time.Now())
		if errors.Is(err, twofactor.ErrInvalidCode) {
			outcomeErr = invalidTwoFactorCodeError()
			return nil
		}
		if err != nil {
			return err
		}
		if err := tx.UpdateTOTPLastUsedCounter(ctx, factor.UserID, counter); err != nil {
			return err
		}
		if err := tx.ReplaceRecoveryCodes(ctx, factor.UserID, hashes); err != nil {
			return err
		}
		_, err = tx.RevokeOtherSessions(ctx, factor.UserID, req.GetCurrentSessionId())
		return err
	})
	if err != nil {
		return nil, err
	}
	if outcomeErr != nil {
		return nil, outcomeErr
	}
	resp := new(authenticatorv1.RegenerateTwoFactorRecoveryCodesResponse)
	resp.SetRecoveryCodes(codes)
	return resp, nil
}

func (s *authenticatorServer) createTwoFactorLoginChallenge(ctx context.Context, userID int64, userAgent, ip string) (*authenticatorv1.TwoFactorLoginChallenge, error) {
	rawToken, err := token.GenerateOpaqueToken()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	expiresAt := now.Add(s.svcCtx.Cfg.TwoFactor.LoginChallengeTTL).UnixMilli()
	if err := s.svcCtx.Store.CreateTwoFactorLoginChallenge(ctx, &model.TwoFactorLoginChallenge{TokenHash: token.Hash(rawToken), UserID: userID, UserAgent: userAgent, IP: ip, CreatedAt: now.UnixMilli(), ExpiresAt: expiresAt}); err != nil {
		return nil, err
	}
	resp := new(authenticatorv1.TwoFactorLoginChallenge)
	resp.SetToken(rawToken)
	resp.SetExpiresAt(expiresAt)
	return resp, nil
}

func (s *authenticatorServer) verifyTOTPCode(factor *model.TOTPFactor, code string, now time.Time) (int64, error) {
	secret, err := s.svcCtx.TwoFactor.Decrypt(factor.UserID, twofactor.Ciphertext{KeyID: factor.EncryptionKeyID, Data: factor.SecretCiphertext})
	if err != nil {
		return 0, err
	}
	defer zeroBytes(secret)
	return twofactor.VerifyCode(secret, code, now, factor.LastUsedCounter)
}

func (s *authenticatorServer) verifyCurrentUserPassword(ctx context.Context, userID int64, plainPassword string) (string, error) {
	getUserReq := new(userv1.GetUserRequest)
	getUserReq.SetUserId(userID)
	getUserResp, err := s.svcCtx.UserClient.GetUser(ctx, getUserReq)
	if err != nil {
		return "", err
	}
	if getUserResp.GetUser() == nil || getUserResp.GetUser().GetUserId() != userID || getUserResp.GetUser().GetEmail() == "" {
		return "", invalidCredentialsError()
	}
	ok, err := s.verifyUserPassword(ctx, userID, plainPassword)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", invalidCredentialsError()
	}
	return getUserResp.GetUser().GetEmail(), nil
}

func (s *authenticatorServer) generateRecoveryCodes() ([]string, []string, error) {
	codes := make([]string, 0, s.svcCtx.Cfg.TwoFactor.RecoveryCodeCount)
	hashes := make([]string, 0, s.svcCtx.Cfg.TwoFactor.RecoveryCodeCount)
	for range s.svcCtx.Cfg.TwoFactor.RecoveryCodeCount {
		code, err := twofactor.GenerateRecoveryCode()
		if err != nil {
			return nil, nil, err
		}
		codes = append(codes, code)
		hashes = append(hashes, token.Hash(twofactor.NormalizeRecoveryCode(code)))
	}
	return codes, hashes, nil
}

func zeroBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
