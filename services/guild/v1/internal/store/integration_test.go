//go:build integration

package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/internal/testpostgres"
	guildmigrations "github.com/soasurs/cordis/services/guild/v1/db/migrations"
)

func TestSQLStoreGuildLifecycle(t *testing.T) {
	ctx := context.Background()
	db := testpostgres.New(t, guildmigrations.Files)
	store := New(db)

	var guildID int64 = 1001
	err := store.Transact(ctx, func(txStore Store) error {
		if _, err := txStore.CreateGuild(ctx, guildID, 2001, "Cordis", "", 10); err != nil {
			return err
		}
		if err := txStore.CreateGuildMember(ctx, guildID, 2001, 10); err != nil {
			return err
		}
		return txStore.CreateDefaultRole(ctx, guildID, 10)
	})
	require.NoError(t, err)

	guild, err := store.GetGuildForMember(ctx, guildID, 2001)
	require.NoError(t, err)
	require.Equal(t, "Cordis", guild.Name)
	require.Equal(t, int64(1), guild.Revision)

	_, err = store.GetGuildForMember(ctx, guildID, 2002)
	require.ErrorIs(t, err, sql.ErrNoRows)

	name := "Renamed"
	updated, err := store.UpdateGuild(ctx, UpdateGuildParams{GuildID: guildID, Name: &name})
	require.NoError(t, err)
	require.Equal(t, "Renamed", updated.Name)
	require.Equal(t, int64(2), updated.Revision)

	guilds, err := store.ListUserGuilds(ctx, ListUserGuildsParams{UserID: 2001, Limit: 50})
	require.NoError(t, err)
	require.Len(t, guilds, 1)

	err = store.Transact(ctx, func(txStore Store) error {
		if _, err := txStore.DeleteGuild(ctx, guildID, 20); err != nil {
			return err
		}
		if err := txStore.DeleteGuildMembers(ctx, guildID, 20); err != nil {
			return err
		}
		return txStore.DeleteGuildRoles(ctx, guildID, 20)
	})
	require.NoError(t, err)
	_, err = store.GetGuildForMember(ctx, guildID, 2001)
	require.ErrorIs(t, err, sql.ErrNoRows)

	var activeMembers, activeRoles int
	require.NoError(t, db.Get(&activeMembers, `SELECT count(*) FROM guild_members WHERE guild_id = $1 AND deleted_at = 0`, guildID))
	require.NoError(t, db.Get(&activeRoles, `SELECT count(*) FROM roles WHERE guild_id = $1 AND deleted_at = 0`, guildID))
	require.Zero(t, activeMembers)
	require.Zero(t, activeRoles)
}
