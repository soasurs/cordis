package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
)

type passwordResetTokenRow struct {
	UserID     int64  `db:"user_id"`
	TokenHash  string `db:"token_hash"`
	CreatedAt  int64  `db:"created_at"`
	ExpiresAt  int64  `db:"expires_at"`
	ConsumedAt int64  `db:"consumed_at"`
}

type emailVerificationTokenRow struct {
	UserID     int64  `db:"user_id"`
	TokenHash  string `db:"token_hash"`
	Email      string `db:"email"`
	CreatedAt  int64  `db:"created_at"`
	ExpiresAt  int64  `db:"expires_at"`
	ConsumedAt int64  `db:"consumed_at"`
}

func (s *SQLStore) UpsertPasswordResetToken(ctx context.Context, token *model.PasswordResetToken) error {
	_, err := s.q.Exec(
		ctx,
		UpsertPasswordResetTokenStatement,
		token.UserID,
		token.TokenHash,
		token.CreatedAt,
		token.ExpiresAt,
	)
	return err
}

func (s *SQLStore) GetPasswordResetToken(ctx context.Context, tokenHash string, forUpdate bool) (*model.PasswordResetToken, error) {
	query := GetPasswordResetTokenQuery
	if forUpdate {
		query += " FOR UPDATE"
	}
	row, err := scanOne(ctx, s.q, query, pgx.RowToStructByName[passwordResetTokenRow], tokenHash)
	if err != nil {
		return nil, err
	}
	return &model.PasswordResetToken{
		UserID:     row.UserID,
		TokenHash:  row.TokenHash,
		CreatedAt:  row.CreatedAt,
		ExpiresAt:  row.ExpiresAt,
		ConsumedAt: row.ConsumedAt,
	}, nil
}

func (s *SQLStore) ConsumePasswordResetToken(ctx context.Context, tokenHash string, consumedAt int64) error {
	tag, err := s.q.Exec(ctx, ConsumePasswordResetTokenStatement, consumedAt, tokenHash)
	if err != nil {
		return err
	}
	return checkRowsAffected(tag)
}

func (s *SQLStore) UpsertEmailVerificationToken(ctx context.Context, token *model.EmailVerificationToken) error {
	_, err := s.q.Exec(
		ctx,
		UpsertEmailVerificationTokenStatement,
		token.UserID,
		token.TokenHash,
		token.Email,
		token.CreatedAt,
		token.ExpiresAt,
	)
	return err
}

func (s *SQLStore) GetEmailVerificationToken(ctx context.Context, tokenHash string, forUpdate bool) (*model.EmailVerificationToken, error) {
	query := GetEmailVerificationTokenQuery
	if forUpdate {
		query += " FOR UPDATE"
	}
	row, err := scanOne(ctx, s.q, query, pgx.RowToStructByName[emailVerificationTokenRow], tokenHash)
	if err != nil {
		return nil, err
	}
	return &model.EmailVerificationToken{
		UserID:     row.UserID,
		TokenHash:  row.TokenHash,
		Email:      row.Email,
		CreatedAt:  row.CreatedAt,
		ExpiresAt:  row.ExpiresAt,
		ConsumedAt: row.ConsumedAt,
	}, nil
}

func (s *SQLStore) ConsumeEmailVerificationToken(ctx context.Context, tokenHash string, consumedAt int64) error {
	tag, err := s.q.Exec(ctx, ConsumeEmailVerificationTokenStatement, consumedAt, tokenHash)
	if err != nil {
		return err
	}
	return checkRowsAffected(tag)
}
