package server

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	"github.com/soasurs/cordis/pkg/password"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/store"
)

// dummyPasswordHash keeps password verification constant-cost when no
// credential exists, so unknown accounts are indistinguishable from wrong
// passwords through timing.
const dummyPasswordHash = "$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// verifyUserPassword checks the supplied password against the locally owned
// credential. A missing credential burns a dummy verification and reports a
// plain mismatch.
func (s *authenticatorServer) verifyUserPassword(ctx context.Context, userID int64, plainPassword string) (bool, error) {
	credential, err := s.svcCtx.Store.GetUserCredential(ctx, userID, false)
	if errors.Is(err, sql.ErrNoRows) {
		if _, verifyErr := s.verifyPassword(ctx, dummyPasswordHash, plainPassword); verifyErr != nil {
			return false, verifyErr
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return s.verifyPassword(ctx, credential.HashedPassword, plainPassword)
}

func (s *authenticatorServer) ChangePassword(ctx context.Context, req *authenticatorv1.ChangePasswordRequest) (*authenticatorv1.ChangePasswordResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "user id is required")
	}
	if req.GetNewPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "new password is required")
	}

	// Hash outside the transaction; only the verification of the old
	// password has to happen under the row lock.
	hashedPassword, err := s.hashPassword(ctx, req.GetNewPassword())
	if err != nil {
		return nil, err
	}

	release, err := s.acquirePasswordSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	now := time.Now().UnixMilli()
	var ok bool
	err = s.svcCtx.Store.Transact(ctx, func(tx store.Store) error {
		credential, err := tx.GetUserCredential(ctx, req.GetUserId(), true)
		if errors.Is(err, sql.ErrNoRows) {
			return status.Error(codes.NotFound, "user not found")
		}
		if err != nil {
			return err
		}
		match, err := password.Verify(credential.HashedPassword, req.GetOldPassword())
		if err != nil {
			return err
		}
		if !match {
			// Mirror the historical contract: a wrong old password is a
			// negative result, not an RPC error, and changes nothing.
			return nil
		}
		if err := tx.UpdateUserCredential(ctx, req.GetUserId(), hashedPassword, now); err != nil {
			return err
		}
		if req.GetCurrentSessionId() > 0 {
			if _, err := tx.RevokeOtherSessions(ctx, req.GetUserId(), req.GetCurrentSessionId()); err != nil {
				return err
			}
		}
		ok = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	resp := new(authenticatorv1.ChangePasswordResponse)
	resp.SetOk(ok)
	return resp, nil
}

func (s *authenticatorServer) hashPassword(ctx context.Context, plainPassword string) (string, error) {
	release, err := s.acquirePasswordSlot(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	return password.Hash(plainPassword)
}

func (s *authenticatorServer) verifyPassword(ctx context.Context, hashedPassword, plainPassword string) (bool, error) {
	release, err := s.acquirePasswordSlot(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	return password.Verify(hashedPassword, plainPassword)
}

func (s *authenticatorServer) acquirePasswordSlot(ctx context.Context) (func(), error) {
	if s.svcCtx.PasswordLimiter == nil {
		return func() {}, nil
	}
	return s.svcCtx.PasswordLimiter.Acquire(ctx, 1)
}
