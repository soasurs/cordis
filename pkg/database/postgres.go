package database

import (
	"errors"
	"fmt"

	"github.com/XSAM/otelsql"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
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

	attrs, err := postgresAttributes(cfg.DataSource)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	sqlDB, err := otelsql.Open("pgx", cfg.DataSource, otelsql.WithAttributes(attrs...))
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	db := sqlx.NewDb(sqlDB, "pgx")
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

func postgresAttributes(dataSource string) ([]attribute.KeyValue, error) {
	cfg, err := pgx.ParseConfig(dataSource)
	if err != nil {
		return nil, err
	}

	attrs := []attribute.KeyValue{semconv.DBSystemNamePostgreSQL}
	if cfg.Host != "" {
		attrs = append(attrs, semconv.ServerAddress(cfg.Host))
	}
	if cfg.Port != 0 {
		attrs = append(attrs, semconv.ServerPort(int(cfg.Port)))
	}
	if cfg.Database != "" {
		attrs = append(attrs, semconv.DBNamespace(cfg.Database))
	}

	return attrs, nil
}
