//go:build integration

package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/migration"
	messagemigrations "github.com/soasurs/cordis/services/message/v1/db/migrations"
)

func TestSQLStoreWithPostgres(t *testing.T) {
	postgres := testkit.StartPostgres(t)
	db, err := database.NewPostgres(database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, migration.Apply(t.Context(), db, messagemigrations.Files))

	store := New(db)
	t.Run("create and get", func(t *testing.T) { testCreateAndGetMessage(t, store) })
	t.Run("list pagination", func(t *testing.T) { testListMessages(t, store) })
	t.Run("update", func(t *testing.T) { testUpdateMessage(t, store) })
	t.Run("delete", func(t *testing.T) { testDeleteMessage(t, store) })
	t.Run("mentions", func(t *testing.T) { testMessageMentions(t, store) })
	t.Run("idempotency", func(t *testing.T) { testMessageIdempotency(t, store) })
	t.Run("transact rollback", func(t *testing.T) { testTransactRollback(t, store) })
	t.Run("constraint enforcement", func(t *testing.T) { testConstraintEnforcement(t, store) })
	t.Run("dm channels", func(t *testing.T) { testDmChannels(t, store) })
	t.Run("read states", func(t *testing.T) { testReadStates(t, store) })
	t.Run("outbox", func(t *testing.T) { testOutbox(t, store, db) })
}
