package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

type Store struct {
	db *sqlx.DB
	q  sqlx.ExtContext
}

func New(db *sqlx.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Transact(ctx context.Context, fn func(txStore *Store) error) error {
	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}

	txStore := &Store{
		db: s.db,
		q:  tx,
	}

	if err := fn(txStore); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}
