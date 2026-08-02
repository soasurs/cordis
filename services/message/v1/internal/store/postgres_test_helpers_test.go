//go:build integration

package store

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

func messageIDs(messages []*model.Message) []int64 {
	out := make([]int64, 0, len(messages))
	for _, message := range messages {
		out = append(out, message.ID)
	}
	return out
}
func ptr[T any](v T) *T { return &v }

func requireCheckViolation(t *testing.T, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "expected pgconn.PgError, got %v", err)
	require.Equal(t, "23514", pgErr.Code)
}
