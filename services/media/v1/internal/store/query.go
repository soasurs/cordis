package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5"
)

// scanOne scans a single row into T and normalizes pgx.ErrNoRows to
// sql.ErrNoRows so callers keep the store's existing not-found contract.
func scanOne[T any](
	ctx context.Context,
	q queryer,
	query string,
	rowTo pgx.RowToFunc[T],
	args ...any,
) (T, error) {
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		var zero T
		return zero, err
	}
	value, err := pgx.CollectOneRow(rows, rowTo)
	if errors.Is(err, pgx.ErrNoRows) {
		return value, sql.ErrNoRows
	}
	return value, err
}

// scanMany scans all rows into []T.
func scanMany[T any](
	ctx context.Context,
	q queryer,
	query string,
	rowTo pgx.RowToFunc[T],
	args ...any,
) ([]T, error) {
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, rowTo)
}
