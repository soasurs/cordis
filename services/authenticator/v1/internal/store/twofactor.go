package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
)

type totpFactorRow struct {
	UserID           int64  `db:"user_id"`
	SecretCiphertext []byte `db:"secret_ciphertext"`
	EncryptionKeyID  string `db:"encryption_key_id"`
	LastUsedCounter  int64  `db:"last_used_counter"`
	EnabledAt        int64  `db:"enabled_at"`
	CreatedAt        int64  `db:"created_at"`
	UpdatedAt        int64  `db:"updated_at"`
}

type totpEnrollmentRow struct {
	UserID           int64  `db:"user_id"`
	TokenHash        string `db:"token_hash"`
	SecretCiphertext []byte `db:"secret_ciphertext"`
	EncryptionKeyID  string `db:"encryption_key_id"`
	CreatedAt        int64  `db:"created_at"`
	ExpiresAt        int64  `db:"expires_at"`
}

type twoFactorLoginChallengeRow struct {
	TokenHash  string `db:"token_hash"`
	UserID     int64  `db:"user_id"`
	UserAgent  string `db:"user_agent"`
	IP         string `db:"ip"`
	Attempts   int    `db:"attempts"`
	CreatedAt  int64  `db:"created_at"`
	ExpiresAt  int64  `db:"expires_at"`
	ConsumedAt int64  `db:"consumed_at"`
}

func (s *SQLStore) GetTOTPFactor(ctx context.Context, userID int64, forUpdate bool) (*model.TOTPFactor, error) {
	query := GetTOTPFactorQuery
	if forUpdate {
		query += " FOR UPDATE"
	}
	row, err := scanOne(ctx, s.q, query, pgx.RowToStructByName[totpFactorRow], userID)
	if err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

func (s *SQLStore) CreateTOTPEnrollment(ctx context.Context, enrollment *model.TOTPEnrollment) error {
	tag, err := s.q.Exec(ctx, CreateTOTPEnrollmentStatement, enrollment.UserID, enrollment.TokenHash, enrollment.SecretCiphertext, enrollment.EncryptionKeyID, enrollment.CreatedAt, enrollment.ExpiresAt, enrollment.CreatedAt)
	if err != nil {
		return err
	}
	return checkRowsAffected(tag)
}

func (s *SQLStore) GetTOTPEnrollment(ctx context.Context, userID int64, tokenHash string, forUpdate bool) (*model.TOTPEnrollment, error) {
	query := GetTOTPEnrollmentQuery
	if forUpdate {
		query += " FOR UPDATE"
	}
	row, err := scanOne(ctx, s.q, query, pgx.RowToStructByName[totpEnrollmentRow], userID, tokenHash)
	if err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

func (s *SQLStore) DeleteTOTPEnrollment(ctx context.Context, userID int64, tokenHash string) error {
	tag, err := s.q.Exec(ctx, DeleteTOTPEnrollmentStatement, userID, tokenHash)
	if err != nil {
		return err
	}
	return checkRowsAffected(tag)
}

func (s *SQLStore) UpsertTOTPFactor(ctx context.Context, factor *model.TOTPFactor) error {
	_, err := s.q.Exec(ctx, UpsertTOTPFactorStatement, factor.UserID, factor.SecretCiphertext, factor.EncryptionKeyID, factor.LastUsedCounter, factor.EnabledAt, factor.CreatedAt, factor.UpdatedAt)
	return err
}

func (s *SQLStore) DeleteTOTPFactor(ctx context.Context, userID int64) error {
	tag, err := s.q.Exec(ctx, DeleteTOTPFactorStatement, userID)
	if err != nil {
		return err
	}
	return checkRowsAffected(tag)
}

func (s *SQLStore) UpdateTOTPLastUsedCounter(ctx context.Context, userID, counter int64) error {
	tag, err := s.q.Exec(ctx, UpdateTOTPLastUsedCounterStatement, counter, time.Now().UnixMilli(), userID, counter)
	if err != nil {
		return err
	}
	return checkRowsAffected(tag)
}

func (s *SQLStore) CreateTwoFactorLoginChallenge(ctx context.Context, challenge *model.TwoFactorLoginChallenge) error {
	_, err := s.q.Exec(ctx, CreateTwoFactorLoginChallengeStatement, challenge.TokenHash, challenge.UserID, challenge.UserAgent, challenge.IP, challenge.Attempts, challenge.CreatedAt, challenge.ExpiresAt, challenge.ConsumedAt)
	return err
}

func (s *SQLStore) GetTwoFactorLoginChallenge(ctx context.Context, tokenHash string, forUpdate bool) (*model.TwoFactorLoginChallenge, error) {
	query := GetTwoFactorLoginChallengeQuery
	if forUpdate {
		query += " FOR UPDATE"
	}
	row, err := scanOne(ctx, s.q, query, pgx.RowToStructByName[twoFactorLoginChallengeRow], tokenHash)
	if err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

func (s *SQLStore) IncrementTwoFactorLoginChallengeAttempts(ctx context.Context, tokenHash string) error {
	tag, err := s.q.Exec(ctx, IncrementTwoFactorLoginChallengeAttemptsStatement, tokenHash)
	if err != nil {
		return err
	}
	return checkRowsAffected(tag)
}

func (s *SQLStore) ConsumeTwoFactorLoginChallenge(ctx context.Context, tokenHash string) error {
	tag, err := s.q.Exec(ctx, ConsumeTwoFactorLoginChallengeStatement, time.Now().UnixMilli(), tokenHash)
	if err != nil {
		return err
	}
	return checkRowsAffected(tag)
}

func (s *SQLStore) ReplaceRecoveryCodes(ctx context.Context, userID int64, codeHashes []string) error {
	if _, err := s.q.Exec(ctx, DeleteRecoveryCodesStatement, userID); err != nil {
		return err
	}
	for _, codeHash := range codeHashes {
		if _, err := s.q.Exec(ctx, CreateRecoveryCodeStatement, userID, codeHash, time.Now().UnixMilli(), 0); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStore) CountUnusedRecoveryCodes(ctx context.Context, userID int64) (int64, error) {
	return scanOne(ctx, s.q, CountUnusedRecoveryCodesQuery, pgx.RowTo[int64], userID)
}

func (s *SQLStore) ConsumeRecoveryCode(ctx context.Context, userID int64, codeHash string) error {
	tag, err := s.q.Exec(ctx, ConsumeRecoveryCodeStatement, time.Now().UnixMilli(), userID, codeHash, 0)
	if err != nil {
		return err
	}
	return checkRowsAffected(tag)
}

func (r *totpFactorRow) toModel() *model.TOTPFactor {
	return &model.TOTPFactor{UserID: r.UserID, SecretCiphertext: r.SecretCiphertext, EncryptionKeyID: r.EncryptionKeyID, LastUsedCounter: r.LastUsedCounter, EnabledAt: r.EnabledAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

func (r *totpEnrollmentRow) toModel() *model.TOTPEnrollment {
	return &model.TOTPEnrollment{UserID: r.UserID, TokenHash: r.TokenHash, SecretCiphertext: r.SecretCiphertext, EncryptionKeyID: r.EncryptionKeyID, CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt}
}

func (r *twoFactorLoginChallengeRow) toModel() *model.TwoFactorLoginChallenge {
	return &model.TwoFactorLoginChallenge{TokenHash: r.TokenHash, UserID: r.UserID, UserAgent: r.UserAgent, IP: r.IP, Attempts: r.Attempts, CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt, ConsumedAt: r.ConsumedAt}
}
