package database

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

type Config struct {
	DataSource   string
	MaxOpenConns int `json:",default=10"`
	MaxIdleConns int `json:",default=5"`
}

func NewPostgres(cfg Config) (*sqlx.DB, error) {
	if cfg.DataSource == "" {
		return nil, errors.New("database data source is required")
	}

	db, err := sqlx.Open("postgres", cfg.DataSource)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return db, nil
}
