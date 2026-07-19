//go:build integration

package store

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/migration"
	guildmigrations "github.com/soasurs/cordis/services/guild/v1/db/migrations"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

// TestSQLStoreWithPostgres shares one PostgreSQL container across all
// integration subtests; each subtest works in its own guild ID space.
func TestSQLStoreWithPostgres(t *testing.T) {
	postgres := testkit.StartPostgres(t)
	db, err := database.NewPostgres(database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, migration.Apply(t.Context(), db, guildmigrations.Files))

	store := New(db)
	t.Run("guild CRUD", func(t *testing.T) { testGuildCRUD(t, store) })
	t.Run("access revision", func(t *testing.T) { testGuildAccessRevision(t, store) })
	t.Run("member lifecycle", func(t *testing.T) { testGuildMemberLifecycle(t, store) })
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
}

func testGuildAccessRevision(t *testing.T, store Store) {
	const guildID, ownerID, memberID, roleID, channelID = int64(19200), int64(29200), int64(29201), int64(19201), int64(19202)
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, ownerID)

	revision := func() int64 {
		guild, err := store.GetGuild(ctx, guildID)
		require.NoError(t, err)
		return guild.AccessRevision
	}
	assertAdvanced := func(previous int64) int64 {
		current := revision()
		require.Greater(t, current, previous)
		return current
	}

	current := revision()
	_, err := store.UpdateGuildMemberNickname(ctx, guildID, ownerID, "owner")
	require.NoError(t, err)
	require.Equal(t, current, revision(), "nickname changes do not affect access")

	_, err = store.CreateGuildMember(ctx, guildID, memberID, now)
	require.NoError(t, err)
	current = assertAdvanced(current)

	_, err = store.CreateGuildRole(ctx, roleID, guildID, "reader", 64, 1, now)
	require.NoError(t, err)
	current = assertAdvanced(current)

	_, err = store.UpdateGuildRole(ctx, UpdateGuildRoleParams{
		GuildID: guildID, RoleID: roleID, Name: ptr("renamed"), UpdatedAt: now,
	})
	require.NoError(t, err)
	current = assertAdvanced(current)

	_, err = store.UpdateGuildRole(ctx, UpdateGuildRoleParams{
		GuildID: guildID, RoleID: roleID, Permissions: ptr(uint64(96)), UpdatedAt: now,
	})
	require.NoError(t, err)
	current = assertAdvanced(current)

	require.NoError(t, store.AddGuildMemberRole(ctx, guildID, memberID, roleID, now))
	current = assertAdvanced(current)

	_, err = store.CreateGuildChannel(ctx, channelID, guildID, "general", 1, 0, "", 0, now)
	require.NoError(t, err)
	current = assertAdvanced(current)

	_, err = store.UpdateGuildChannel(ctx, UpdateGuildChannelParams{
		ChannelID: channelID, Name: ptr("chat"), UpdatedAt: now,
	})
	require.NoError(t, err)
	require.Equal(t, current, revision(), "channel metadata does not affect access")

	_, err = store.UpsertGuildChannelPermissionOverwrite(ctx, &model.ChannelPermissionOverwrite{
		ChannelID: channelID, GuildID: guildID, TargetType: 2, TargetID: memberID,
		Deny: 64, CreatedAt: now,
	})
	require.NoError(t, err)
	current = assertAdvanced(current)

	require.NoError(t, store.DeleteGuildChannelPermissionOverwrite(ctx, channelID, 2, memberID))
	current = assertAdvanced(current)

	_, err = store.TransferGuildOwnership(ctx, guildID, ownerID, memberID)
	require.NoError(t, err)
	assertAdvanced(current)
}

func testResourceQuotas(t *testing.T, store Store) {
	const ownerID = int64(29001)
	ctx := t.Context()
	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)

	for i := range 2 {
		guildID := int64(19001 + i)
		go func() {
			ready.Done()
			<-start
			results <- store.Transact(ctx, func(txStore Store) error {
				if err := txStore.CheckResourceQuota(ctx, ResourceQuota{
					Kind: QuotaOwnedGuilds, ScopeID: ownerID, Limit: 1,
				}); err != nil {
					return err
				}
				_, err := txStore.CreateGuild(ctx, guildID, ownerID, "quota", "", time.Now().UnixMilli())
				return err
			})
		}()
	}
	ready.Wait()
	close(start)

	errs := []error{<-results, <-results}
	var succeeded, exhausted int
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrResourceLimitExceeded):
			exhausted++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, exhausted)

	const guildID, memberID, channelID = int64(19100), int64(29100), int64(39100)
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, memberID)
	_, err := store.CreateGuildChannel(ctx, channelID, guildID, "general", 1, 0, "", 0, now)
	require.NoError(t, err)
	_, err = store.CreateGuildInvite(ctx, &model.GuildInvite{
		ID: 49100, Code: "quota-active", GuildID: guildID, CreatorUserID: memberID, CreatedAt: now,
	})
	require.NoError(t, err)
	_, err = store.CreateGuildInvite(ctx, &model.GuildInvite{
		ID: 49101, Code: "quota-expired", GuildID: guildID, CreatorUserID: memberID, ExpiresAt: now - 1, CreatedAt: now,
	})
	require.NoError(t, err)
	_, err = store.CreateGuildInvite(ctx, &model.GuildInvite{
		ID: 49102, Code: "quota-exhausted", GuildID: guildID, CreatorUserID: memberID, MaxUses: 1, CreatedAt: now,
	})
	require.NoError(t, err)
	_, err = store.ConsumeGuildInvite(ctx, "quota-exhausted", now)
	require.NoError(t, err)
	_, err = store.UpsertGuildChannelPermissionOverwrite(ctx, &model.ChannelPermissionOverwrite{
		ChannelID: channelID, GuildID: guildID, TargetType: 1, TargetID: guildID, CreatedAt: now,
	})
	require.NoError(t, err)

	check := func(quota ResourceQuota) error {
		return store.Transact(ctx, func(txStore Store) error {
			return txStore.CheckResourceQuota(ctx, quota)
		})
	}
	require.ErrorIs(t, check(ResourceQuota{Kind: QuotaOwnedGuilds, ScopeID: memberID, Limit: 1}), ErrResourceLimitExceeded)
	require.ErrorIs(t, check(ResourceQuota{Kind: QuotaJoinedGuilds, ScopeID: memberID, Limit: 1}), ErrResourceLimitExceeded)
	require.ErrorIs(t, check(ResourceQuota{
		Kind: QuotaJoinedGuilds, ScopeID: memberID, Limit: 1, TargetID: guildID,
	}), ErrMemberAlreadyExists)
	require.ErrorIs(t, check(ResourceQuota{Kind: QuotaGuildRoles, ScopeID: guildID, Limit: 1}), ErrResourceLimitExceeded)
	require.ErrorIs(t, check(ResourceQuota{Kind: QuotaGuildChannels, ScopeID: guildID, Limit: 1}), ErrResourceLimitExceeded)
	require.ErrorIs(t, check(ResourceQuota{Kind: QuotaActiveInvites, ScopeID: guildID, Limit: 1, Now: now}), ErrResourceLimitExceeded)
	require.NoError(t, check(ResourceQuota{
		Kind: QuotaChannelOverwrites, ScopeID: channelID, Limit: 1, TargetType: 1, TargetID: guildID,
	}))
	require.ErrorIs(t, check(ResourceQuota{
		Kind: QuotaChannelOverwrites, ScopeID: channelID, Limit: 1, TargetType: 2, TargetID: memberID,
	}), ErrResourceLimitExceeded)
}

func testGuildCRUD(t *testing.T, store Store) {
	const guildID, ownerID = 10100, 20100
	ctx := t.Context()
	now := time.Now().UnixMilli()

	_, err := store.CreateGuild(ctx, guildID, ownerID, "Cordis", "icon", now)
	require.NoError(t, err)
	_, err = store.CreateGuildMember(ctx, guildID, ownerID, now)
	require.NoError(t, err)
	require.NoError(t, store.CreateDefaultRole(ctx, guildID, now))

	g, err := store.GetGuildForMember(ctx, guildID, ownerID)
	require.NoError(t, err)
	require.Equal(t, "Cordis", g.Name)
	require.Equal(t, "icon", g.IconURI)
	require.Equal(t, int64(1), g.Revision)

	gu, err := store.UpdateGuild(ctx, UpdateGuildParams{GuildID: guildID, Name: ptr("Updated")})
	require.NoError(t, err)
	require.Equal(t, "Updated", gu.Name)
	require.Equal(t, int64(2), gu.Revision)

	gu, err = store.UpdateGuild(ctx, UpdateGuildParams{GuildID: guildID, IconURI: ptr("icon2")})
	require.NoError(t, err)
	require.Equal(t, "icon2", gu.IconURI)
	require.Equal(t, int64(3), gu.Revision)

	const guildID2 = 10101
	seedGuild(t, store, guildID2, ownerID)
	list, err := store.ListUserGuilds(ctx, ListUserGuildsParams{UserID: ownerID, Limit: 1})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, int64(guildID2), list[0].ID)
	list, err = store.ListUserGuilds(ctx, ListUserGuildsParams{UserID: ownerID, Before: list[0].ID, Limit: 1})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, int64(guildID), list[0].ID)

	dg, err := store.DeleteGuild(ctx, guildID, now)
	require.NoError(t, err)
	require.Equal(t, int64(4), dg.Revision)
	require.True(t, dg.DeletedAt > 0)
	_, err = store.DeleteGuild(ctx, guildID, now)
	require.ErrorIs(t, err, sql.ErrNoRows)
	_, err = store.GetGuildForMember(ctx, guildID, ownerID)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func testGuildMemberLifecycle(t *testing.T, store Store) {
	const guildID, ownerID, memberID2 = 10200, 20200, 20201
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, ownerID)

	member, err := store.GetGuildMember(ctx, guildID, ownerID)
	require.NoError(t, err)
	require.Equal(t, int64(1), member.Revision)

	dup, err := store.CreateGuildMember(ctx, guildID, ownerID, now)
	require.ErrorIs(t, err, ErrMemberAlreadyExists)
	require.Nil(t, dup)

	m2, err := store.CreateGuildMember(ctx, guildID, memberID2, now)
	require.NoError(t, err)
	require.Equal(t, int64(memberID2), m2.UserID)
	require.Equal(t, int64(1), m2.Revision)

	updated, err := store.UpdateGuildMemberNickname(ctx, guildID, ownerID, "Ada")
	require.NoError(t, err)
	require.Equal(t, "Ada", updated.Nickname)
	require.Equal(t, int64(2), updated.Revision)

	members, err := store.ListGuildMembers(ctx, ListGuildMembersParams{GuildID: guildID, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, []int64{memberID2, ownerID}, idsOf(members, func(m *model.GuildMember) int64 { return m.UserID }))

	members, err = store.ListGuildMembers(ctx, ListGuildMembersParams{GuildID: guildID, BeforeUserID: memberID2, Limit: 1})
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.Equal(t, int64(ownerID), members[0].UserID)

	removed, err := store.RemoveGuildMember(ctx, guildID, memberID2, now)
	require.NoError(t, err)
	require.True(t, removed.DeletedAt > 0)
	_, err = store.GetGuildMember(ctx, guildID, memberID2)
	require.ErrorIs(t, err, sql.ErrNoRows)

	rejoined, err := store.CreateGuildMember(ctx, guildID, memberID2, now)
	require.NoError(t, err)
	require.Equal(t, int64(0), rejoined.DeletedAt)
	require.Equal(t, int64(3), rejoined.Revision)
	require.Equal(t, "", rejoined.Nickname)

	_, err = store.UpsertGuildBan(ctx, &model.GuildBan{
		GuildID: guildID, UserID: memberID2, ActorUserID: ownerID,
		Reason: "violation", CreatedAt: now,
	})
	require.NoError(t, err)
	_, err = store.RemoveGuildMember(ctx, guildID, memberID2, now)
	require.NoError(t, err)
	_, err = store.CreateGuildMember(ctx, guildID, memberID2, now)
	require.ErrorIs(t, err, ErrMemberAlreadyExists)
}

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

	bans, err = store.ListGuildBans(ctx, ListGuildBansParams{GuildID: guildID, BeforeUserID: bannedID, Limit: 1})
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

func testTransferGuildOwnership(t *testing.T, store Store) {
	const guildID, ownerID, memberID = 10400, 20400, 20401
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, ownerID)
	_, err := store.CreateGuildMember(ctx, guildID, memberID, now)
	require.NoError(t, err)

	gu, err := store.TransferGuildOwnership(ctx, guildID, ownerID, memberID)
	require.NoError(t, err)
	require.Equal(t, int64(memberID), gu.OwnerID)
	require.Equal(t, int64(2), gu.Revision)

	_, err = store.TransferGuildOwnership(ctx, guildID, ownerID, memberID)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func testGuildRolesCRUD(t *testing.T, store Store) {
	const guildID, ownerID = 10500, 20500
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, ownerID)

	roles, err := store.ListGuildRoles(ctx, guildID)
	require.NoError(t, err)
	require.Len(t, roles, 1)
	require.Equal(t, int64(guildID), roles[0].ID)
	require.True(t, roles[0].IsDefault)
	require.Equal(t, int32(0), roles[0].Position)
	// VIEW_CHANNEL | SEND_MESSAGES | CREATE_INVITE
	require.Equal(t, uint64(1120), roles[0].Permissions)

	mod, err := store.CreateGuildRole(ctx, 10501, guildID, "Mod", 1024, 5, now)
	require.NoError(t, err)
	require.Equal(t, "Mod", mod.Name)
	require.Equal(t, uint64(1024), mod.Permissions)
	require.Equal(t, int32(5), mod.Position)

	_, err = store.CreateGuildRole(ctx, 10502, guildID, "Admin", 8, 10, now)
	require.NoError(t, err)
	roles, err = store.ListGuildRoles(ctx, guildID)
	require.NoError(t, err)
	require.Equal(t, []int64{10502, 10501, guildID}, idsOf(roles, func(r *model.Role) int64 { return r.ID }))

	updated, err := store.UpdateGuildRole(ctx, UpdateGuildRoleParams{
		GuildID: guildID, RoleID: 10501, Name: ptr("Moderator"),
		Permissions: ptr(uint64(2048)), UpdatedAt: now,
	})
	require.NoError(t, err)
	require.Equal(t, "Moderator", updated.Name)
	require.Equal(t, uint64(2048), updated.Permissions)
	require.Equal(t, int64(2), updated.Revision)

	moved, err := store.UpdateGuildRolePosition(ctx, guildID, 10501, 15, now)
	require.NoError(t, err)
	require.Equal(t, int32(15), moved.Position)

	_, err = store.UpdateGuildRolePosition(ctx, guildID, guildID, 100, now)
	require.ErrorIs(t, err, sql.ErrNoRows)

	del, err := store.DeleteGuildRole(ctx, guildID, 10502, now)
	require.NoError(t, err)
	require.True(t, del.DeletedAt > 0)
	_, err = store.GetGuildRole(ctx, guildID, 10502)
	require.ErrorIs(t, err, sql.ErrNoRows)
	_, err = store.DeleteGuildRole(ctx, guildID, guildID, now)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func testGuildMemberRoles(t *testing.T, store Store) {
	const guildID, ownerID, memberID = 10600, 20600, 20601
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, ownerID)

	_, err := store.CreateGuildRole(ctx, 10601, guildID, "Mod", 1, 1, now)
	require.NoError(t, err)
	_, err = store.CreateGuildRole(ctx, 10602, guildID, "Tmp", 2, 2, now)
	require.NoError(t, err)

	memberRoles, err := store.ListGuildMemberRoles(ctx, guildID, ownerID)
	require.NoError(t, err)
	require.Len(t, memberRoles, 1)
	require.True(t, memberRoles[0].IsDefault)

	require.NoError(t, store.AddGuildMemberRole(ctx, guildID, ownerID, 10601, now))
	require.NoError(t, store.AddGuildMemberRole(ctx, guildID, ownerID, 10601, now))
	memberRoles, err = store.ListGuildMemberRoles(ctx, guildID, ownerID)
	require.NoError(t, err)
	require.Equal(t, []int64{10601, guildID}, idsOf(memberRoles, func(r *model.Role) int64 { return r.ID }))

	require.NoError(t, store.RemoveGuildMemberRole(ctx, guildID, ownerID, 10601))
	memberRoles, err = store.ListGuildMemberRoles(ctx, guildID, ownerID)
	require.NoError(t, err)
	require.Len(t, memberRoles, 1)

	require.NoError(t, store.AddGuildMemberRole(ctx, guildID, ownerID, 10601, now))
	require.NoError(t, store.AddGuildMemberRole(ctx, guildID, ownerID, 10602, now))
	require.NoError(t, store.DeleteGuildMemberRoleAssignments(ctx, guildID, ownerID))
	memberRoles, err = store.ListGuildMemberRoles(ctx, guildID, ownerID)
	require.NoError(t, err)
	require.Len(t, memberRoles, 1)

	_, err = store.CreateGuildMember(ctx, guildID, memberID, now)
	require.NoError(t, err)
	require.NoError(t, store.AddGuildMemberRole(ctx, guildID, memberID, 10601, now))
	require.NoError(t, store.AddGuildMemberRole(ctx, guildID, ownerID, 10601, now))
	require.NoError(t, store.AddGuildMemberRole(ctx, guildID, ownerID, 10602, now))
	require.NoError(t, store.DeleteGuildRoleAssignments(ctx, guildID, 10601))
	memberRoles, err = store.ListGuildMemberRoles(ctx, guildID, ownerID)
	require.NoError(t, err)
	require.Len(t, memberRoles, 2)
	memberRoles, err = store.ListGuildMemberRoles(ctx, guildID, memberID)
	require.NoError(t, err)
	require.Len(t, memberRoles, 1)

	require.NoError(t, store.AddGuildMemberRole(ctx, guildID, ownerID, 10601, now))
	require.NoError(t, store.DeleteAllGuildRoleAssignments(ctx, guildID))
	memberRoles, err = store.ListGuildMemberRoles(ctx, guildID, ownerID)
	require.NoError(t, err)
	require.Len(t, memberRoles, 1)
	memberRoles, err = store.ListGuildMemberRoles(ctx, guildID, memberID)
	require.NoError(t, err)
	require.Len(t, memberRoles, 1)
}

func testGuildChannels(t *testing.T, store Store) {
	const guildID, ownerID = 10700, 20700
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, ownerID)

	cat, err := store.CreateGuildChannel(ctx, 10701, guildID, "Category", 3, 0, "", 0, now)
	require.NoError(t, err)
	require.Equal(t, int32(3), cat.Type)

	txt, err := store.CreateGuildChannel(ctx, 10702, guildID, "general", 1, 1, "welcome", cat.ID, now)
	require.NoError(t, err)
	require.Equal(t, "welcome", txt.Topic)
	require.Equal(t, cat.ID, txt.ParentID)

	_, err = store.CreateGuildChannel(ctx, 10703, guildID, "Voice", 2, 2, "", 0, now)
	require.NoError(t, err)

	loaded, err := store.GetGuildChannel(ctx, 10702)
	require.NoError(t, err)
	require.Equal(t, "general", loaded.Name)

	channels, err := store.ListGuildChannels(ctx, guildID)
	require.NoError(t, err)
	require.Equal(t, []int64{10701, 10702, 10703}, idsOf(channels, func(c *model.Channel) int64 { return c.ID }))

	updated, err := store.UpdateGuildChannel(ctx, UpdateGuildChannelParams{
		ChannelID: 10702, Topic: ptr("desc"), ParentID: ptr(int64(0)), UpdatedAt: now,
	})
	require.NoError(t, err)
	require.Equal(t, "desc", updated.Topic)
	require.Equal(t, int64(0), updated.ParentID)
	require.Equal(t, int64(2), updated.Revision)

	moved, err := store.UpdateGuildChannelPosition(ctx, 10702, 5, now)
	require.NoError(t, err)
	require.Equal(t, int32(5), moved.Position)

	deleted, err := store.DeleteGuildChannel(ctx, cat.ID, now)
	require.NoError(t, err)
	require.True(t, deleted.DeletedAt > 0)
	_, err = store.GetGuildChannel(ctx, cat.ID)
	require.ErrorIs(t, err, sql.ErrNoRows)

	_, err = store.UpdateGuildChannel(ctx, UpdateGuildChannelParams{
		ChannelID: 10703, ParentID: ptr(cat.ID), UpdatedAt: now,
	})
	require.NoError(t, err)
	require.NoError(t, store.ClearGuildChannelParent(ctx, guildID, cat.ID, now))
	ch, err := store.GetGuildChannel(ctx, 10703)
	require.NoError(t, err)
	require.Equal(t, int64(0), ch.ParentID)

	require.NoError(t, store.DeleteGuildChannels(ctx, guildID, now))
	channels, err = store.ListGuildChannels(ctx, guildID)
	require.NoError(t, err)
	require.Empty(t, channels)
}

func testGuildChannelOverwrites(t *testing.T, store Store) {
	const guildID, ownerID, channelID, channel2ID = 10800, 20800, 10801, 10802
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, ownerID)
	_, err := store.CreateGuildChannel(ctx, channelID, guildID, "ch", 1, 0, "", 0, now)
	require.NoError(t, err)

	ow, err := store.UpsertGuildChannelPermissionOverwrite(ctx, &model.ChannelPermissionOverwrite{
		ChannelID: channelID, GuildID: guildID, TargetType: 1, TargetID: 20801,
		Allow: 1024, Deny: 0, CreatedAt: now,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1024), ow.Allow)
	require.Equal(t, int64(1), ow.Revision)

	_, err = store.UpsertGuildChannelPermissionOverwrite(ctx, &model.ChannelPermissionOverwrite{
		ChannelID: channelID, GuildID: guildID, TargetType: 2, TargetID: 20802,
		Allow: 0, Deny: 2048, CreatedAt: now,
	})
	require.NoError(t, err)

	ow2, err := store.UpsertGuildChannelPermissionOverwrite(ctx, &model.ChannelPermissionOverwrite{
		ChannelID: channelID, GuildID: guildID, TargetType: 1, TargetID: 20801,
		Allow: 4096, Deny: 0, CreatedAt: now,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(4096), ow2.Allow)
	require.Equal(t, int64(2), ow2.Revision)

	ows, err := store.ListGuildChannelPermissionOverwrites(ctx, channelID)
	require.NoError(t, err)
	require.Len(t, ows, 2)
	require.Equal(t, int32(1), ows[0].TargetType)
	require.Equal(t, int32(2), ows[1].TargetType)

	require.NoError(t, store.DeleteGuildChannelPermissionOverwrite(ctx, channelID, 1, 20801))
	ows, err = store.ListGuildChannelPermissionOverwrites(ctx, channelID)
	require.NoError(t, err)
	require.Len(t, ows, 1)

	require.NoError(t, store.DeleteGuildChannelPermissionOverwrites(ctx, channelID))
	ows, err = store.ListGuildChannelPermissionOverwrites(ctx, channelID)
	require.NoError(t, err)
	require.Empty(t, ows)

	_, err = store.CreateGuildChannel(ctx, channel2ID, guildID, "ch2", 1, 1, "", 0, now)
	require.NoError(t, err)
	for _, ch := range []int64{channelID, channel2ID} {
		_, err = store.UpsertGuildChannelPermissionOverwrite(ctx, &model.ChannelPermissionOverwrite{
			ChannelID: ch, GuildID: guildID, TargetType: 1, TargetID: 20899,
			Allow: 1, Deny: 0, CreatedAt: now,
		})
		require.NoError(t, err)
		_, err = store.UpsertGuildChannelPermissionOverwrite(ctx, &model.ChannelPermissionOverwrite{
			ChannelID: ch, GuildID: guildID, TargetType: 2, TargetID: 20898,
			Allow: 2, Deny: 0, CreatedAt: now,
		})
		require.NoError(t, err)
	}
	ows, err = store.ListGuildChannelPermissionOverwritesByGuild(ctx, guildID)
	require.NoError(t, err)
	require.Len(t, ows, 4)
	require.Equal(t, int64(channelID), ows[0].ChannelID)
	require.Equal(t, int64(channelID), ows[1].ChannelID)
	require.Equal(t, int64(channel2ID), ows[2].ChannelID)
	require.Equal(t, int64(channel2ID), ows[3].ChannelID)
	require.NoError(t, store.DeleteGuildChannelPermissionOverwritesForTarget(ctx, guildID, 1, 20899))
	for _, ch := range []int64{channelID, channel2ID} {
		ows, err = store.ListGuildChannelPermissionOverwrites(ctx, ch)
		require.NoError(t, err)
		require.Len(t, ows, 1)
		require.Equal(t, int64(20898), ows[0].TargetID)
	}

	require.NoError(t, store.DeleteAllGuildChannelPermissionOverwrites(ctx, guildID))
	for _, ch := range []int64{channelID, channel2ID} {
		ows, err = store.ListGuildChannelPermissionOverwrites(ctx, ch)
		require.NoError(t, err)
		require.Empty(t, ows)
	}
}

func testTransactRollback(t *testing.T, store Store) {
	const guildID, ownerID = 10900, 20900
	ctx := t.Context()
	now := time.Now().UnixMilli()

	err := store.Transact(ctx, func(tx Store) error {
		if _, err := tx.CreateGuild(ctx, guildID, ownerID, "G", "", now); err != nil {
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
		ChannelID: channelID, GuildID: guildID, TargetType: 1, TargetID: 21001,
		Allow: 3, Deny: 1, CreatedAt: now,
	})
	requireCheckViolation(t, err)

	_, err = store.CreateGuild(ctx, 0, ownerID, "G", "", now)
	requireCheckViolation(t, err)

	_, err = store.CreateGuild(ctx, 11002, ownerID, strings.Repeat("x", 101), "", now)
	requireCheckViolation(t, err)

	_, err = store.CreateGuildChannel(ctx, 11003, guildID, "ch", 9, 0, "", 0, now)
	requireCheckViolation(t, err)

	err = store.CreateDefaultRole(ctx, guildID, now)
	var pqErr *pq.Error
	require.True(t, errors.As(err, &pqErr))
	require.Equal(t, pq.ErrorCode("23505"), pqErr.Code)
}

func testGuildDeleteHelpers(t *testing.T, store Store) {
	const guildID, ownerID = 11100, 21100
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, ownerID)
	_, err := store.CreateGuildRole(ctx, 11101, guildID, "R", 1, 1, now)
	require.NoError(t, err)

	require.NoError(t, store.DeleteGuildMembers(ctx, guildID, now))
	_, err = store.GetGuildMember(ctx, guildID, ownerID)
	require.ErrorIs(t, err, sql.ErrNoRows)

	require.NoError(t, store.DeleteGuildRoles(ctx, guildID, now))
	roles, err := store.ListGuildRoles(ctx, guildID)
	require.NoError(t, err)
	require.Empty(t, roles)
}

// seedGuild creates a guild with its owner membership and @everyone default
// role, mirroring the CreateGuild RPC transaction.
func seedGuild(t *testing.T, store Store, guildID, ownerID int64) {
	t.Helper()
	now := time.Now().UnixMilli()
	require.NoError(t, store.Transact(t.Context(), func(tx Store) error {
		if _, err := tx.CreateGuild(t.Context(), guildID, ownerID, "Guild", "", now); err != nil {
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
	var pqErr *pq.Error
	require.True(t, errors.As(err, &pqErr), "expected pq.Error, got %v", err)
	require.Equal(t, pq.ErrorCode("23514"), pqErr.Code)
}

func ptr[T any](v T) *T { return &v }

func idsOf[T any](items []T, id func(T) int64) []int64 {
	out := make([]int64, 0, len(items))
	for _, item := range items {
		out = append(out, id(item))
	}
	return out
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
	var pqErr *pq.Error
	require.True(t, errors.As(err, &pqErr))
	require.Equal(t, pq.ErrorCode("23505"), pqErr.Code)

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
