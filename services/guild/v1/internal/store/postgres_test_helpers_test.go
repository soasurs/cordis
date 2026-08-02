//go:build integration

package store

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func countTrue(values []bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}
func seedGuild(t *testing.T, store Store, guildID, ownerID int64) {
	t.Helper()
	now := time.Now().UnixMilli()
	require.NoError(t, store.Transact(t.Context(), func(tx Store) error {
		if _, err := tx.CreateGuild(t.Context(), guildID, ownerID, "Guild", now); err != nil {
			return err
		}
		if _, err := tx.CreateGuildMember(t.Context(), guildID, ownerID, now); err != nil {
			return err
		}
		return tx.CreateDefaultRole(t.Context(), guildID, now)
	}))
}

func requireCheckViolation(t *testing.T, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "expected pgconn.PgError, got %v", err)
	require.Equal(t, "23514", pgErr.Code)
}

func ptr[T any](v T) *T { return &v }

func idsOf[T any](items []T, id func(T) int64) []int64 {
	out := make([]int64, 0, len(items))
	for _, item := range items {
		out = append(out, id(item))
	}
	return out
}
