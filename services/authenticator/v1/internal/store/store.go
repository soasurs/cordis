package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"

	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
)

type Store interface {
	CreateSession(ctx context.Context, sessionID, userID int64, refreshTokenHash, userAgent, ip string, expiresAt int64) (*model.Session, error)
	GetSession(ctx context.Context, sessionID int64) (*model.Session, error)
	RotateRefreshToken(ctx context.Context, sessionID int64, oldRefreshTokenHash, newRefreshTokenHash string) error
	RevokeSession(ctx context.Context, sessionID int64) error
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
