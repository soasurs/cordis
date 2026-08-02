//go:build integration

package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

func testGuildBans(t *testing.T, store Store) {
	const guildID, ownerID, bannedID = 10300, 20300, 20301
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, ownerID)

	ban, err := store.UpsertGuildBan(ctx, &model.GuildBan{
		GuildID: guildID, UserID: bannedID, ActorUserID: ownerID,
		Reason: "spam", CreatedAt: now,
	})
	require.NoError(t, err)
	require.Equal(t, "spam", ban.Reason)

	ban2, err := store.UpsertGuildBan(ctx, &model.GuildBan{
		GuildID: guildID, UserID: bannedID, ActorUserID: ownerID,
		Reason: "harassment", CreatedAt: now + 1,
	})
	require.NoError(t, err)
	require.Equal(t, "harassment", ban2.Reason)
	require.Equal(t, now+1, ban2.CreatedAt)

	loaded, err := store.GetGuildBan(ctx, guildID, bannedID)
	require.NoError(t, err)
	require.Equal(t, "harassment", loaded.Reason)

	bans, err := store.ListGuildBans(ctx, ListGuildBansParams{GuildID: guildID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, bans, 1)

	bans, err = store.ListGuildBans(ctx, ListGuildBansParams{
		GuildID: guildID, BeforeCreatedAt: bans[0].CreatedAt, BeforeUserID: bannedID, Limit: 1,
	})
	require.NoError(t, err)
	require.Empty(t, bans)

	require.NoError(t, store.DeleteGuildBan(ctx, guildID, bannedID))
	_, err = store.GetGuildBan(ctx, guildID, bannedID)
	require.ErrorIs(t, err, sql.ErrNoRows)
	require.ErrorIs(t, store.DeleteGuildBan(ctx, guildID, bannedID), sql.ErrNoRows)

	_, err = store.UpsertGuildBan(ctx, &model.GuildBan{
		GuildID: guildID, UserID: bannedID, ActorUserID: ownerID,
		Reason: "x", CreatedAt: now,
	})
	require.NoError(t, err)
	require.NoError(t, store.DeleteGuildBans(ctx, guildID))
	bans, err = store.ListGuildBans(ctx, ListGuildBansParams{GuildID: guildID, Limit: 10})
	require.NoError(t, err)
	require.Empty(t, bans)
}

func testGuildInvites(t *testing.T, store Store) {
	const guildID, ownerID, memberID = 11200, 21200, 21201
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, ownerID)

	// The @everyone role created by seedGuild grants CREATE_INVITE.
	roles, err := store.ListGuildRoles(ctx, guildID)
	require.NoError(t, err)
	require.Len(t, roles, 1)
	require.NotZero(t, roles[0].Permissions&1024)

	created, err := store.CreateGuildInvite(ctx, &model.GuildInvite{
		ID: 11201, Code: "int-invite-a", GuildID: guildID, CreatorUserID: ownerID,
		MaxUses: 2, ExpiresAt: 0, CreatedAt: now,
	})
	require.NoError(t, err)
	require.Equal(t, "int-invite-a", created.Code)
	require.Zero(t, created.Uses)

	_, err = store.CreateGuildInvite(ctx, &model.GuildInvite{
		ID: 11202, Code: "int-invite-b", GuildID: guildID, CreatorUserID: ownerID,
		MaxUses: 0, ExpiresAt: now + 60_000, CreatedAt: now,
	})
	require.NoError(t, err)

	// Duplicate codes violate the unique index.
	_, err = store.CreateGuildInvite(ctx, &model.GuildInvite{
		ID: 11203, Code: "int-invite-a", GuildID: guildID, CreatorUserID: ownerID, CreatedAt: now,
	})
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr))
	require.Equal(t, "23505", pgErr.Code)

	loaded, err := store.GetGuildInvite(ctx, "int-invite-a")
	require.NoError(t, err)
	require.Equal(t, int64(11201), loaded.ID)
	_, err = store.GetGuildInvite(ctx, "int-invite-missing")
	require.ErrorIs(t, err, sql.ErrNoRows)

	invites, err := store.ListGuildInvites(ctx, ListGuildInvitesParams{GuildID: guildID, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, []int64{11202, 11201}, idsOf(invites, func(invite *model.GuildInvite) int64 { return invite.ID }))
	invites, err = store.ListGuildInvites(ctx, ListGuildInvitesParams{GuildID: guildID, BeforeID: 11202, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, []int64{11201}, idsOf(invites, func(invite *model.GuildInvite) int64 { return invite.ID }))

	// Consuming respects the max-use budget.
	consumed, err := store.ConsumeGuildInvite(ctx, "int-invite-a", now)
	require.NoError(t, err)
	require.Equal(t, int32(1), consumed.Uses)
	consumed, err = store.ConsumeGuildInvite(ctx, "int-invite-a", now)
	require.NoError(t, err)
	require.Equal(t, int32(2), consumed.Uses)
	_, err = store.ConsumeGuildInvite(ctx, "int-invite-a", now)
	require.ErrorIs(t, err, sql.ErrNoRows)

	// Expired invites cannot be consumed.
	_, err = store.ConsumeGuildInvite(ctx, "int-invite-b", now+120_000)
	require.ErrorIs(t, err, sql.ErrNoRows)
	consumed, err = store.ConsumeGuildInvite(ctx, "int-invite-b", now)
	require.NoError(t, err)
	require.Equal(t, int32(1), consumed.Uses)

	// A failed transaction rolls the consumed use back.
	sentinel := errors.New("abort join")
	err = store.Transact(ctx, func(tx Store) error {
		if _, err := tx.ConsumeGuildInvite(ctx, "int-invite-b", now); err != nil {
			return err
		}
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	loaded, err = store.GetGuildInvite(ctx, "int-invite-b")
	require.NoError(t, err)
	require.Equal(t, int32(1), loaded.Uses)

	// GetGuild and CountGuildMembers back the invite preview.
	guild, err := store.GetGuild(ctx, guildID)
	require.NoError(t, err)
	require.Equal(t, int64(guildID), guild.ID)
	_, err = store.GetGuild(ctx, guildID+99)
	require.ErrorIs(t, err, sql.ErrNoRows)

	count, err := store.CountGuildMembers(ctx, guildID)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	_, err = store.CreateGuildMember(ctx, guildID, memberID, now)
	require.NoError(t, err)
	count, err = store.CountGuildMembers(ctx, guildID)
	require.NoError(t, err)
	require.Equal(t, int64(2), count)
	_, err = store.RemoveGuildMember(ctx, guildID, memberID, now)
	require.NoError(t, err)
	count, err = store.CountGuildMembers(ctx, guildID)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	require.NoError(t, store.DeleteGuildInvite(ctx, "int-invite-a"))
	require.ErrorIs(t, store.DeleteGuildInvite(ctx, "int-invite-a"), sql.ErrNoRows)

	require.NoError(t, store.DeleteGuildInvites(ctx, guildID))
	invites, err = store.ListGuildInvites(ctx, ListGuildInvitesParams{GuildID: guildID, Limit: 10})
	require.NoError(t, err)
	require.Empty(t, invites)
}
