//go:build integration

package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

func testTransactRollback(t *testing.T, store Store) {
	const guildID, ownerID = 10900, 20900
	ctx := t.Context()
	now := time.Now().UnixMilli()

	err := store.Transact(ctx, func(tx Store) error {
		if _, err := tx.CreateGuild(ctx, guildID, ownerID, "G", now); err != nil {
			return err
		}
		if _, err := tx.CreateGuildMember(ctx, guildID, ownerID, now); err != nil {
			return err
		}
		return errors.New("force rollback")
	})
	require.Error(t, err)
	_, err = store.GetGuildForMember(ctx, guildID, ownerID)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func testConstraintEnforcement(t *testing.T, store Store) {
	const guildID, ownerID, channelID = 11000, 21000, 11001
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, ownerID)
	_, err := store.CreateGuildChannel(ctx, channelID, guildID, "ch", 1, 0, "", 0, now)
	require.NoError(t, err)

	_, err = store.UpsertGuildChannelPermissionOverwrite(ctx, &model.ChannelPermissionOverwrite{
		ChannelID: channelID, GuildID: guildID, AppliesTo: 1, AppliesToID: 21001,
		Allow: 3, Deny: 1, CreatedAt: now,
	})
	requireCheckViolation(t, err)

	_, err = store.CreateGuild(ctx, 0, ownerID, "G", now)
	requireCheckViolation(t, err)

	_, err = store.CreateGuild(ctx, 11002, ownerID, strings.Repeat("x", 101), now)
	requireCheckViolation(t, err)

	_, err = store.CreateGuildChannel(ctx, 11003, guildID, "ch", 9, 0, "", 0, now)
	requireCheckViolation(t, err)

	err = store.CreateDefaultRole(ctx, guildID, now)
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr))
	require.Equal(t, "23505", pgErr.Code)
}
