//go:build integration

package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/migration"
	guildmigrations "github.com/soasurs/cordis/services/guild/v1/db/migrations"
)

func TestSQLStoreWithPostgres(t *testing.T) {
	postgres := testkit.StartPostgres(t)
	migrationDB, err := database.NewPostgres(database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, migrationDB.Close()) })
	db, err := database.NewPostgresPool(t.Context(), database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(db.Close)
	require.NoError(t, migration.Apply(t.Context(), migrationDB, guildmigrations.Files))

	store := New(db)
	t.Run("guild CRUD", func(t *testing.T) { testGuildCRUD(t, store) })
	t.Run("access revision", func(t *testing.T) { testGuildAccessRevision(t, store) })
	t.Run("channel layout revision", func(t *testing.T) { testGuildChannelLayoutRevision(t, store) })
	t.Run("member lifecycle", func(t *testing.T) { testGuildMemberLifecycle(t, store) })
	t.Run("member profile search", func(t *testing.T) { testGuildMemberProfileSearch(t, store) })
	t.Run("common guild membership", func(t *testing.T) { testCommonGuildMembership(t, store) })
	t.Run("bans", func(t *testing.T) { testGuildBans(t, store) })
	t.Run("ownership transfer", func(t *testing.T) { testTransferGuildOwnership(t, store) })
	t.Run("roles CRUD", func(t *testing.T) { testGuildRolesCRUD(t, store) })
	t.Run("member roles", func(t *testing.T) { testGuildMemberRoles(t, store) })
	t.Run("channels", func(t *testing.T) { testGuildChannels(t, store) })
	t.Run("channel overwrites", func(t *testing.T) { testGuildChannelOverwrites(t, store) })
	t.Run("transact rollback", func(t *testing.T) { testTransactRollback(t, store) })
	t.Run("constraint enforcement", func(t *testing.T) { testConstraintEnforcement(t, store) })
	t.Run("guild delete helpers", func(t *testing.T) { testGuildDeleteHelpers(t, store) })
	t.Run("invites", func(t *testing.T) { testGuildInvites(t, store) })
	t.Run("resource quotas", func(t *testing.T) { testResourceQuotas(t, store) })
	t.Run("channel mutation lock", func(t *testing.T) { testGuildChannelMutationLock(t, store) })
	t.Run("idempotency", func(t *testing.T) { testGuildIdempotency(t, store) })
}
