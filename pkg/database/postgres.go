package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/XSAM/otelsql"
	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

// NewPostgresPool opens a native pgx connection pool with the same semantic
// connection attributes used by NewPostgres. Unlike NewPostgres it does not go
// through database/sql, so query tracing is provided by otelpgx.
func NewPostgresPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	if cfg.DataSource == "" {
		return nil, errors.New("database data source is required")
	}

	attrs, err := postgresAttributes(cfg.DataSource)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	poolConfig, err := pgxpool.ParseConfig(cfg.DataSource)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	poolConfig.ConnConfig.Tracer = otelpgx.NewTracer(
		otelpgx.WithTracerAttributes(attrs...),
		otelpgx.WithTrimSQLInSpanName(),
		otelpgx.WithDisableSQLStatementInAttributes(),
		otelpgx.WithDisableConnectionDetailsInAttributes(),
		otelpgx.WithDisableAcquireTracer(),
	)
	if cfg.MaxOpenConns > 0 {
		poolConfig.MaxConns = int32(cfg.MaxOpenConns)
	}
	// pgxpool has no "max idle" setting; treat the configured idle count as
	// the number of idle connections the pool should keep available.
	if cfg.MaxIdleConns > 0 {
		poolConfig.MinIdleConns = int32(cfg.MaxIdleConns)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
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
