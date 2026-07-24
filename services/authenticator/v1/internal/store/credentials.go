package store

import (
	"context"

	"github.com/jmoiron/sqlx"

	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
)

type userCredentialRow struct {
	UserID         int64  `db:"user_id"`
	HashedPassword string `db:"hashed_password"`
	CreatedAt      int64  `db:"created_at"`
	UpdatedAt      int64  `db:"updated_at"`
}

// CreateUserCredential inserts the credential for a user that has none yet.
// It reports sql.ErrNoRows when a credential already exists so registration
// can distinguish a fresh insert from a lost race.
func (s *SQLStore) CreateUserCredential(ctx context.Context, credential *model.UserCredential) error {
	res, err := s.q.ExecContext(
		ctx,
		CreateUserCredentialStatement,
		credential.UserID,
		credential.HashedPassword,
		credential.CreatedAt,
	)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}

func (s *SQLStore) GetUserCredential(ctx context.Context, userID int64, forUpdate bool) (*model.UserCredential, error) {
	query := GetUserCredentialQuery
	if forUpdate {
		query += " FOR UPDATE"
	}
	row := new(userCredentialRow)
	if err := sqlx.GetContext(ctx, s.q, row, query, userID); err != nil {
		return nil, err
	}
	return &model.UserCredential{
		UserID:         row.UserID,
		HashedPassword: row.HashedPassword,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}, nil
}

func (s *SQLStore) UpdateUserCredential(ctx context.Context, userID int64, hashedPassword string, updatedAt int64) error {
	res, err := s.q.ExecContext(ctx, UpdateUserCredentialStatement, hashedPassword, updatedAt, userID)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}
