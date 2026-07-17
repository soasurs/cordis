package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"

	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
)

type Store interface {
	Transact(ctx context.Context, fn func(Store) error) error
	CreateSession(ctx context.Context, sessionID, userID int64, refreshTokenHash, userAgent, ip string, expiresAt int64) (*model.Session, error)
	GetSession(ctx context.Context, sessionID int64) (*model.Session, error)
	ListSessions(ctx context.Context, userID int64) ([]*model.Session, error)
	RotateRefreshToken(ctx context.Context, sessionID int64, oldRefreshTokenHash, newRefreshTokenHash string) error
	RevokeSession(ctx context.Context, sessionID int64) error
	RevokeUserSession(ctx context.Context, userID, sessionID int64) error
	RevokeOtherSessions(ctx context.Context, userID, currentSessionID int64) (int64, error)
	GetTOTPFactor(ctx context.Context, userID int64, forUpdate bool) (*model.TOTPFactor, error)
	CreateTOTPEnrollment(ctx context.Context, enrollment *model.TOTPEnrollment) error
	GetTOTPEnrollment(ctx context.Context, userID int64, tokenHash string, forUpdate bool) (*model.TOTPEnrollment, error)
	DeleteTOTPEnrollment(ctx context.Context, userID int64, tokenHash string) error
	UpsertTOTPFactor(ctx context.Context, factor *model.TOTPFactor) error
	DeleteTOTPFactor(ctx context.Context, userID int64) error
	UpdateTOTPLastUsedCounter(ctx context.Context, userID, counter int64) error
	CreateTwoFactorLoginChallenge(ctx context.Context, challenge *model.TwoFactorLoginChallenge) error
	GetTwoFactorLoginChallenge(ctx context.Context, tokenHash string, forUpdate bool) (*model.TwoFactorLoginChallenge, error)
	IncrementTwoFactorLoginChallengeAttempts(ctx context.Context, tokenHash string) error
	ConsumeTwoFactorLoginChallenge(ctx context.Context, tokenHash string) error
	// ReplaceRecoveryCodes must be called within a Store transaction.
	ReplaceRecoveryCodes(ctx context.Context, userID int64, codeHashes []string) error
	CountUnusedRecoveryCodes(ctx context.Context, userID int64) (int64, error)
	ConsumeRecoveryCode(ctx context.Context, userID int64, codeHash string) error
}

type SQLStore struct {
	db *sqlx.DB
	q  sqlx.ExtContext
}

func New(db *sqlx.DB) Store {
	return &SQLStore{
		db: db,
		q:  db,
	}
}

func (s *SQLStore) Transact(ctx context.Context, fn func(Store) error) (err error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = tx.Rollback()
			panic(recovered)
		}
		if err != nil {
			_ = tx.Rollback()
			return
		}
		err = tx.Commit()
	}()

	return fn(&SQLStore{db: s.db, q: tx})
}

func checkRowsAffected(res sql.Result) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
